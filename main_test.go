package main

import (
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/eladser/mtop/internal/proxy"
)

func TestHistoryRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")

	// newest first, the order the store hands out
	want := []proxy.Request{
		{Model: "c", When: time.Unix(3, 0)},
		{Model: "b", When: time.Unix(2, 0)},
		{Model: "a", When: time.Unix(1, 0)},
	}
	writeHistory(path, want)

	got := readHistory(path)
	if len(got) != 3 || got[0].Model != "c" || got[2].Model != "a" {
		t.Fatalf("round trip changed order: %+v", got)
	}
}

func TestReadHistoryCaps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	f, _ := os.Create(path)
	for i := 0; i < historyMax+50; i++ {
		f.WriteString(`{"Model":"m"}` + "\n")
	}
	f.Close()

	if got := readHistory(path); len(got) != historyMax {
		t.Fatalf("expected cap at %d, got %d", historyMax, len(got))
	}
}

func TestReadHistoryMissingFile(t *testing.T) {
	if got := readHistory(filepath.Join(t.TempDir(), "nope.jsonl")); got != nil {
		t.Fatalf("missing file should be nil, got %v", got)
	}
}

// a line written before Ctx/Overflow existed on Request should still
// decode fine, just with those fields at their zero value.
func TestReadHistoryDecodesOldLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	old := `{"When":"2026-01-01T00:00:00Z","Path":"/api/generate","Model":"llama3:8b","PromptTk":12,"OutTk":100,"TokSec":50.5}` + "\n"
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}

	got := readHistory(path)
	if len(got) != 1 {
		t.Fatalf("expected 1 request, got %d", len(got))
	}
	r := got[0]
	if r.Model != "llama3:8b" || r.PromptTk != 12 || r.OutTk != 100 {
		t.Fatalf("old fields didn't decode: %+v", r)
	}
	if r.Ctx != 0 || r.Overflow {
		t.Fatalf("new fields should default to zero value: %+v", r)
	}
}

func TestHasFlag(t *testing.T) {
	tests := []struct {
		args []string
		want bool
	}{
		{[]string{"--demo"}, true},
		{[]string{"-demo"}, true},
		{[]string{"-demo=true"}, true},
		{[]string{"-ollama", "http://x:1", "--demo"}, true},
		{[]string{"-demo=false"}, false},
		{[]string{"demo"}, false},
		{[]string{"-ollama", "demo"}, false},
		{nil, false},
	}
	for _, tt := range tests {
		if got := hasFlag(tt.args, "demo"); got != tt.want {
			t.Errorf("hasFlag(%q) = %v, want %v", tt.args, got, tt.want)
		}
	}
}

// the listen address from the environment must not be used or opened in demo
func TestDemoOpensNoListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	t.Setenv("MTOP_LISTEN", addr)
	t.Setenv("MTOP_OLLAMA", "http://127.0.0.1:1")
	t.Setenv("MTOP_GPU", "nvidia")
	conf["MTOP_LISTEN"] = addr
	t.Cleanup(func() { delete(conf, "MTOP_LISTEN") })

	app, _, store := newDemo()
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	app.Update(app.Init()())
	out := stripANSI(app.View().Content)

	if c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond); err == nil {
		c.Close()
		t.Fatalf("something is listening on %s", addr)
	}
	for _, want := range []string{"MODELS", "DEMO: simulated data", "Simulated GPU (24 GB)", "GPU  DEMO", "qwen3:32b"} {
		if !strings.Contains(out, want) {
			t.Errorf("demo view missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "proxy on") || strings.Contains(out, "nvidia") {
		t.Errorf("demo view shows real wiring:\n%s", out)
	}
	if store.Err() != nil {
		t.Errorf("store err: %v", store.Err())
	}
}

var ansiRe = regexp.MustCompile(string(rune(27)) + "[[][0-9;]*m")

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }
