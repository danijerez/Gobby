package main

import "testing"

func TestRingBufferKeepsLastLine(t *testing.T) {
	var b ringBuffer
	b.Write([]byte("progress spam\rmore spam\n"))
	b.Write([]byte("Error: codec not supported\n"))
	if got := b.String(); got != "Error: codec not supported" {
		t.Errorf("got %q", got)
	}
}

func TestNeedsRemux(t *testing.T) {
	cases := map[string]bool{
		"a.mkv": true, "a.MKV": true, "a.avi": true,
		"a.mp4": false, "a.webm": false, "a.mov": false,
	}
	for path, want := range cases {
		if needsRemux(path) != want {
			t.Errorf("needsRemux(%q)=%v, want %v", path, !want, want)
		}
	}
}

func TestProbeRegexes(t *testing.T) {
	sample := `  Duration: 01:53:07.53, start: 0.000000, bitrate: 5003 kb/s
  Stream #0:0: Video: h264 (High), yuv420p(progressive), 1920x1034
  Stream #0:1(spa): Audio: ac3, 48000 Hz, 5.1(side), fltp, 384 kb/s`

	if m := reProbeVideo.FindStringSubmatch(sample); m == nil || m[1] != "h264" {
		t.Errorf("video: got %v, want h264", m)
	}
	if m := reProbeAudio.FindStringSubmatch(sample); m == nil || m[1] != "ac3" {
		t.Errorf("audio: got %v, want ac3", m)
	}
	m := reProbeDur.FindStringSubmatch(sample)
	if m == nil {
		t.Fatal("duration not matched")
	}

	if m[1] != "01" || m[2] != "53" || m[3] != "07" || m[4] != "53" {
		t.Errorf("duration parts: %v", m[1:])
	}
}

func TestParseDurationAndTracks(t *testing.T) {
	sample := `  Duration: 01:53:07.53, start: 0.000000, bitrate: 5003 kb/s
  Stream #0:0: Video: h264 (High), yuv420p, 1920x1034
  Stream #0:1(spa): Audio: ac3, 48000 Hz, 5.1
  Stream #0:2(eng): Audio: aac, 48000 Hz, stereo
  Stream #0:3(spa): Subtitle: subrip`

	if got := parseDuration(sample); got < 6787.52 || got > 6787.54 {
		t.Errorf("parseDuration=%v, want ~6787.53", got)
	}
	tr := parseTracks(sample)
	if len(tr.Audio) != 2 || tr.Audio[0].Lang != "spa" || tr.Audio[1].Idx != 1 {
		t.Errorf("audio tracks: %+v", tr.Audio)
	}
	if len(tr.Subs) != 1 || tr.Subs[0].Lang != "spa" {
		t.Errorf("sub tracks: %+v", tr.Subs)
	}
}
