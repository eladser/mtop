package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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
