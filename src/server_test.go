package main

import (
	"net"
	"net/http"
	"testing"
)

func TestRequestIsLocal(t *testing.T) {
	mk := func(addr string, cfRay bool) *http.Request {
		r, _ := http.NewRequest("POST", "http://x/api/upload", nil)
		r.RemoteAddr = addr
		if cfRay {
			r.Header.Set("Cf-Ray", "abc")
		}
		return r
	}
	cases := []struct {
		name  string
		addr  string
		cf    bool
		local bool
	}{
		{"loopback", "127.0.0.1:5000", false, true},
		{"ipv6 loopback", "[::1]:5000", false, true},
		{"lan", "192.168.1.20:5000", false, false},
		{"tunnel via loopback", "127.0.0.1:5000", true, false},
	}
	for _, c := range cases {
		if got := requestIsLocal(mk(c.addr, c.cf)); got != c.local {
			t.Errorf("%s: got %v want %v", c.name, got, c.local)
		}
	}

	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok || n.IP.IsLoopback() || n.IP.To4() == nil {
				continue
			}
			if !requestIsLocal(mk(n.IP.String()+":5000", false)) {
				t.Errorf("own IP %s not treated as local", n.IP)
			}
			break
		}
	}
}

func TestTunnelKeyOK(t *testing.T) {
	const want = "deadbeef"
	mk := func(query, cookie string) *http.Request {
		r, _ := http.NewRequest("GET", "http://x/"+query, nil)
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: "gobby_k", Value: cookie})
		}
		return r
	}
	cases := []struct {
		name        string
		query, cook string
		ok          bool
	}{
		{"query match", "?k=deadbeef", "", true},
		{"cookie match", "", "deadbeef", true},
		{"query wrong", "?k=nope", "", false},
		{"cookie wrong", "", "nope", false},
		{"nothing", "", "", false},
	}
	for _, c := range cases {
		if got := tunnelKeyOK(mk(c.query, c.cook), want); got != c.ok {
			t.Errorf("%s: got %v want %v", c.name, got, c.ok)
		}
	}
}
