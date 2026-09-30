package ui

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eladser/mtop/internal/gpu"
	"github.com/eladser/mtop/internal/proxy"
	"github.com/eladser/mtop/internal/sources"
)

var update = flag.Bool("update", false, "update golden files")

// fixedApp builds a deterministic App: a few models across two sources
// (one fully offloaded, one partial-offload, one CPU-only), one GPU with
// a two-sample history so the sparkline renders, and a handful of
// requests including a heuristic ctx marker and a confirmed overflow.
func fixedApp() *App {
	rows := []sources.Row{
		{Name: "qwen3:0.6b", From: "ollama", Size: "0.6b", Quant: "Q4_K_M", VRAM: 1 << 30, Ctx: 4096, Expires: time.Now().Add(25 * time.Minute)},
		{Name: "qwen3:32b", From: "ollama", Size: "32b", Quant: "Q4_K_M", VRAM: 8 << 30, CPU: 38, Ctx: 4096, Expires: time.Now().Add(4*time.Minute + 12*time.Second)},
		{Name: "phi-4", From: "lm studio", Size: "14b", Quant: "Q8_0", VRAM: 0, CPU: 100},
	}

	store := proxy.NewStore(256)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	// distinct request counts per model (3/2/1) so byModel's sort by
	// count isn't deciding ties by map iteration order
	store.Preload([]proxy.Request{
		{When: base.Add(5 * time.Second), Model: "qwen3:32b", PromptTk: 3980, Ctx: 4096, TokSec: 42.3, OutTk: 128, Total: 910 * time.Millisecond},
		{When: base.Add(4 * time.Second), Model: "phi-4", Overflow: true, PromptTk: 5000, Ctx: 4096, TokSec: 12.1, OutTk: 8, Total: 300 * time.Millisecond},
		{When: base.Add(3 * time.Second), Model: "qwen3:0.6b", PromptTk: 50, Ctx: 4096, TokSec: 88.0, OutTk: 40, Total: 120 * time.Millisecond},
		{When: base.Add(2 * time.Second), Model: "qwen3:0.6b", PromptTk: 60, Ctx: 4096, TokSec: 90.0, OutTk: 42, Total: 110 * time.Millisecond},
		{When: base.Add(1 * time.Second), Model: "phi-4", PromptTk: 40, Ctx: 4096, TokSec: 15.0, OutTk: 10, Total: 280 * time.Millisecond},
		{When: base, Model: "qwen3:0.6b", PromptTk: 45, Ctx: 4096, TokSec: 85.0, OutTk: 38, Total: 115 * time.Millisecond},
	})

	a := &App{
		gpu:       gpu.New(),
		store:     store,
		listen:    "127.0.0.1:4321",
		version:   "test",
		memAlert:  93,
		tempAlert: 87,
		w:         100,
		h:         30,
		sel:       1,
		alive:     []string{"ollama", "lm studio"},
		disk:      2,
		rows:      rows,
		gpus: []gpu.Stats{
			{Name: "NVIDIA RTX 4090", Util: 62, MemUsed: 18000, MemTotal: 24576, Temp: 71, Power: 320.5},
		},
		gpuHist: map[string]*trace{
			"NVIDIA RTX 4090": {util: []float64{58, 62}, mem: []float64{70, 73}},
		},
	}
	return a
}

func TestGoldenView(t *testing.T) {
	tests := []struct {
		name   string
		golden string
		tweak  func(*App)
	}{
		{name: "requests", golden: "testdata/view.golden"},
		{name: "by model", golden: "testdata/view_bymodel.golden", tweak: func(a *App) { a.byModel = true }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := fixedApp()
			if tt.tweak != nil {
				tt.tweak(a)
			}
			got := a.View()
			if *update {
				if err := os.MkdirAll(filepath.Dir(tt.golden), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(tt.golden, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(tt.golden)
			if err != nil {
				t.Fatalf("read golden: %v (run with -update to create it)", err)
			}
			if got != string(want) {
				t.Errorf("view mismatch, run with -update to see the diff\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
			}
		})
	}
}
