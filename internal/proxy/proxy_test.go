package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func proxyFor(t *testing.T, upstream string, store *Store) *httptest.Server {
	t.Helper()
	p, err := New(upstream, store)
	if err != nil {
		t.Fatal(err)
	}
	front := httptest.NewServer(p.Handler())
	t.Cleanup(front.Close)
	return front
}

func TestStreamingChunks(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		io.WriteString(w, `{"model":"llama3:8b","done":false,"response":"hi"}`+"\n")
		io.WriteString(w, `{"model":"llama3:8b","done":true,"prompt_eval_count":12,"eval_count":100,"eval_duration":2000000000,"total_duration":2500000000}`+"\n")
	}))
	defer upstream.Close()

	store := NewStore(10)
	front := proxyFor(t, upstream.URL, store)

	resp, err := http.Post(front.URL+"/api/generate", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), `"response":"hi"`) {
		t.Fatalf("client should see the stream untouched, got: %s", body)
	}

	reqs := store.Recent(1)
	if len(reqs) != 1 {
		t.Fatalf("expected 1 recorded request, got %d", len(reqs))
	}
	r := reqs[0]
	if r.Model != "llama3:8b" || r.OutTk != 100 || r.PromptTk != 12 {
		t.Fatalf("bad record: %+v", r)
	}
	// 100 tokens over 2s
	if r.TokSec < 49.9 || r.TokSec > 50.1 {
		t.Fatalf("bad tok/s: %f", r.TokSec)
	}
}

func TestNonStreamingNoTrailingNewline(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"model":"qwen3:4b","done":true,"prompt_eval_count":5,"eval_count":40,"eval_duration":1000000000,"total_duration":1200000000}`)
	}))
	defer upstream.Close()

	store := NewStore(10)
	front := proxyFor(t, upstream.URL, store)

	resp, err := http.Post(front.URL+"/api/chat", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()

	reqs := store.Recent(1)
	if len(reqs) != 1 || reqs[0].OutTk != 40 || reqs[0].Path != "/api/chat" {
		t.Fatalf("bad record: %+v", reqs)
	}
}

func TestOtherPathsPassThrough(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"models":[]}`)
	}))
	defer upstream.Close()

	store := NewStore(10)
	front := proxyFor(t, upstream.URL, store)

	resp, err := http.Get(front.URL + "/api/tags")
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()

	if got := store.Recent(1); len(got) != 0 {
		t.Fatalf("tags should not be recorded: %+v", got)
	}
}
