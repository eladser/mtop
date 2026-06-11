package compare

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Errorf("path %s", r.URL.Path)
		}
		w.Write([]byte(`{"eval_count":100,"eval_duration":2000000000,"total_duration":2500000000}`))
	}))
	defer srv.Close()

	rs := Run(srv.URL, "hi", []string{"a", "b"})
	if len(rs) != 2 {
		t.Fatalf("want 2, got %d", len(rs))
	}
	if rs[0].Model != "a" || rs[0].OutTk != 100 {
		t.Fatalf("bad result: %+v", rs[0])
	}
	if rs[0].TokSec < 49.9 || rs[0].TokSec > 50.1 {
		t.Fatalf("tok/s %f", rs[0].TokSec)
	}
}

func TestRunRecordsErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no such model", http.StatusNotFound)
	}))
	defer srv.Close()

	rs := Run(srv.URL, "hi", []string{"ghost"})
	if rs[0].Err == nil {
		t.Fatal("expected an error recorded")
	}
	if !strings.Contains(Table(rs), "ghost") {
		t.Fatal("table should still list the failed model")
	}
}
