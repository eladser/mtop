package ollama

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const psFixture = `{"models":[{"name":"llama3:8b","size":6654289920,"size_vram":6654289920,"expires_at":"2026-06-09T19:00:00.000000000+03:00","details":{"parameter_size":"8.0B","quantization_level":"Q4_0"}}]}`

func TestLoaded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ps" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Write([]byte(psFixture))
	}))
	defer srv.Close()

	models, err := New(srv.URL).Loaded()
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].Name != "llama3:8b" {
		t.Fatalf("bad parse: %+v", models)
	}
	if models[0].Details.QuantizationLevel != "Q4_0" {
		t.Fatalf("bad details: %+v", models[0].Details)
	}
	if models[0].SizeVRAM != 6654289920 {
		t.Fatalf("bad vram: %d", models[0].SizeVRAM)
	}
}

func TestUnload(t *testing.T) {
	var gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Write([]byte(`{"done":true}`))
	}))
	defer srv.Close()

	if err := New(srv.URL).Unload("qwen3:0.6b"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/generate" {
		t.Fatalf("wrong path: %s", gotPath)
	}
	if !strings.Contains(gotBody, `"keep_alive":0`) || !strings.Contains(gotBody, `"qwen3:0.6b"`) {
		t.Fatalf("wrong body: %s", gotBody)
	}
}

func TestUnloadServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()

	if err := New(srv.URL).Unload("x"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestOverdue(t *testing.T) {
	past := Model{ExpiresAt: time.Now().Add(-time.Minute)}
	future := Model{ExpiresAt: time.Now().Add(time.Minute)}
	var zero Model
	if !past.Overdue() {
		t.Fatal("past expiry should be overdue")
	}
	if future.Overdue() || zero.Overdue() {
		t.Fatal("future/zero expiry should not be overdue")
	}
}
