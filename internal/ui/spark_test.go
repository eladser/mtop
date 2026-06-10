package ui

import "testing"

func TestSparkline(t *testing.T) {
	if got := sparkline([]float64{0, 50, 100}, 10); got != "▁▄█" {
		t.Fatalf("got %q", got)
	}
	if got := sparkline(nil, 10); got != "" {
		t.Fatalf("empty input should be empty, got %q", got)
	}
	if got := sparkline([]float64{0, 0}, 10); got != "▁▁" {
		t.Fatalf("all-zero should be flat, got %q", got)
	}
	// width clamp keeps the most recent values
	if got := sparkline([]float64{1, 2, 100}, 2); len([]rune(got)) != 2 {
		t.Fatalf("expected 2 runes, got %q", got)
	}
}

func TestTrunc(t *testing.T) {
	if got := trunc("short", 10); got != "short" {
		t.Fatalf("got %q", got)
	}
	if got := trunc("llama-3.1-8b-instruct.Q4_K_M.gguf", 22); got != "llama-3.1-8b-instruct…" || len([]rune(got)) != 22 {
		t.Fatalf("got %q (%d runes)", got, len([]rune(got)))
	}
}
