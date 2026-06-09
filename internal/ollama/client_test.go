package ollama

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
