package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/eladser/mtop/internal/proxy"
	"github.com/eladser/mtop/internal/sources"
)

func TestModelLine(t *testing.T) {
	a := &App{}
	tests := []struct {
		name string
		row  sources.Row
		want []string // substrings the line must contain
	}{
		{
			name: "full gpu",
			row:  sources.Row{Name: "qwen3:0.6b", From: "ollama", VRAM: 1 << 30, Quant: "Q4_K_M"},
			want: []string{"1.0G"},
		},
		{
			name: "partial offload shows cpu percent and ttl",
			row:  sources.Row{Name: "qwen3:32b", From: "ollama", VRAM: 8 << 30, CPU: 38, Expires: time.Now().Add(4*time.Minute + 12*time.Second)},
			want: []string{"8.0G", "cpu 38%"},
		},
		{
			name: "fully on cpu shows cpu in vram cell",
			row:  sources.Row{Name: "qwen3:70b", From: "ollama", VRAM: 0, CPU: 100},
			want: []string{"cpu"},
		},
		{
			name: "non-ollama source with no vram info shows a dash",
			row:  sources.Row{Name: "phi-4", From: "lm studio", VRAM: 0},
			want: []string{"—"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line := a.modelLine(tt.row, false)
			for _, w := range tt.want {
				if !strings.Contains(line, w) {
					t.Fatalf("line %q missing %q", line, w)
				}
			}
		})
	}
}

func TestPartialCPU(t *testing.T) {
	tests := []struct {
		cpu  int
		want bool
	}{
		{0, false},
		{1, true},
		{38, true},
		{99, true},
		{100, false},
	}
	for _, tt := range tests {
		if got := partialCPU(sources.Row{CPU: tt.cpu}); got != tt.want {
			t.Fatalf("cpu=%d: got %v want %v", tt.cpu, got, tt.want)
		}
	}
}

func TestCtxMarker(t *testing.T) {
	tests := []struct {
		name string
		req  proxy.Request
		want string
	}{
		{"no ctx known", proxy.Request{PromptTk: 100}, ""},
		{"low usage", proxy.Request{PromptTk: 100, Ctx: 4096}, ""},
		{"heuristic high usage", proxy.Request{PromptTk: 3980, Ctx: 4096}, "ctx 97%"},
		{"confirmed rejection", proxy.Request{Overflow: true, PromptTk: 5000, Ctx: 4096}, "ctx over, rejected"},
		{"rejection wins even with a low ctx percent", proxy.Request{Overflow: true, PromptTk: 10, Ctx: 4096}, "ctx over, rejected"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ctxMarker(tt.req); got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}
