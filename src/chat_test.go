package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestArgsString(t *testing.T) {
	mk := func(raw string) toolCall {
		var tc toolCall
		tc.Function.Args = json.RawMessage(raw)
		return tc
	}
	cases := map[string]string{
		`"{\"query\":\"zorbo\"}"`: `{"query":"zorbo"}`,
		`{"query":"zorbo"}`:      `{"query":"zorbo"}`,
		``:                          `{}`,
		`  {"kind":"movie"}  `:      `{"kind":"movie"}`,
	}
	for in, want := range cases {
		if got := mk(in).argsString(); got != want {
			t.Errorf("argsString(%q)=%q want %q", in, got, want)
		}
	}
}

func TestMarshalToolCallMsg(t *testing.T) {
	m := chatMsg{Role: "assistant", ToolCalls: []toolCall{{ID: "1", Type: "function"}}}
	b, _ := json.Marshal(m)
	if !strings.Contains(string(b), `"content":null`) {
		t.Errorf("assistant+tool_calls must send content:null, got %s", b)
	}
	plain := chatMsg{Role: "user", Content: "hi"}
	b2, _ := json.Marshal(plain)
	if !strings.Contains(string(b2), `"content":"hi"`) {
		t.Errorf("plain msg content, got %s", b2)
	}
}
