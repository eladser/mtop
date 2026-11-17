package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/eladser/mtop/internal/gpu"
)

func TestDemoTitleBudget(t *testing.T) {
	tests := []struct {
		name  string
		w     int
		alive []string
		disk  int
	}{
		{"demo sources, 100", 100, []string{"demo"}, 7},
		{"demo sources, 80", 80, []string{"demo"}, 7},
		{"three servers, 100", 100, []string{"ollama", "llama.cpp", "lm studio"}, 12},
		{"three servers, 80", 80, []string{"ollama", "llama.cpp", "lm studio"}, 12},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &App{w: tt.w, h: 30, alive: tt.alive, disk: tt.disk, demo: true}
			title := strings.Split(stripANSI(a.modelsPane((a.w-6)/2)), "\n")[0]
			if lineW := (a.w-6)/2 - 4; len([]rune(title)) > lineW {
				t.Fatalf("title %q is wider than %d", title, lineW)
			}
			if !strings.HasPrefix(title, "MODELS") || !strings.HasSuffix(title, "  DEMO") {
				t.Fatalf("title %q lost MODELS or the DEMO tag", title)
			}
		})
	}

	a := &App{w: 100, h: 30, alive: []string{"demo"}, disk: 7, demo: true}
	if got, want := strings.Split(stripANSI(a.modelsPane(47)), "\n")[0], "MODELS  7 on disk  DEMO"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	// the tag comes out of the budget first: the disk count goes before DEMO does
	a = &App{w: 80, h: 30, alive: []string{"ollama", "llama.cpp", "lm studio"}, disk: 12, demo: true}
	got := strings.Split(stripANSI(a.modelsPane(37)), "\n")[0]
	if strings.Contains(got, "on disk") || !strings.HasSuffix(got, "…  DEMO") {
		t.Fatalf("title %q should cut the servers and keep the tag", got)
	}
}

func TestDemoGPUTitle(t *testing.T) {
	t.Setenv("MTOP_GPU", "apple")
	a := &App{gpu: gpu.New(), demo: true}
	if first := strings.Split(stripANSI(a.gpuPane()), "\n")[0]; first != "GPU  DEMO" {
		t.Fatalf("got %q", first)
	}
	a.demo = false
	if first := strings.Split(stripANSI(a.gpuPane()), "\n")[0]; first != "GPU" {
		t.Fatalf("got %q", first)
	}
}

func TestStatusPrecedence(t *testing.T) {
	hot := []gpu.Stats{{Name: "g", Temp: 90}}
	tests := []struct {
		name  string
		demo  bool
		gpus  []gpu.Stats
		flash string
		want  string // substring
		not   string
	}{
		{"alert beats flash and demo", true, hot, "unloaded x", "g is at 90°C", "DEMO"},
		{"flash then demo text", true, nil, "unloaded x", "unloaded x · DEMO: simulated data, no server or GPU needed · q quit", "proxy"},
		{"demo text alone", true, nil, "", "DEMO: simulated data, no server or GPU needed · q quit", "↑/↓"},
		{"stale flash falls back to demo", true, nil, "old", "DEMO: simulated", "old"},
		{"not demo keeps the help", false, nil, "", "↑/↓ select", "DEMO"},
		{"not demo flash", false, nil, "unloaded x", "unloaded x ·", "DEMO"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &App{demo: tt.demo, gpus: tt.gpus, memAlert: 93, tempAlert: 87, version: "t", flash: tt.flash, flashOk: true, flashAt: time.Now()}
			if tt.name == "stale flash falls back to demo" {
				a.flashAt = time.Now().Add(-time.Minute)
			}
			got := stripANSI(a.statusLine())
			if !strings.Contains(got, tt.want) || strings.Contains(got, tt.not) {
				t.Fatalf("got %q, want %q without %q", got, tt.want, tt.not)
			}
		})
	}
}
