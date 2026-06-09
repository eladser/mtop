// Package proxy sits between any ollama client and the real server.
// Ollama has no metrics endpoint; the only place per-request numbers
// exist is the response stream itself, where the final chunk carries
// token counts and timings. So we forward traffic untouched and read
// the chunks as they pass through.
package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

type Proxy struct {
	target *url.URL
	store  *Store
}

func New(upstream string, store *Store) (*Proxy, error) {
	u, err := url.Parse(upstream)
	if err != nil {
		return nil, err
	}
	return &Proxy{target: u, store: store}, nil
}

func (p *Proxy) Handler() http.Handler {
	rp := httputil.NewSingleHostReverseProxy(p.target)
	dir := rp.Director
	rp.Director = func(req *http.Request) {
		dir(req)
		// keep bodies plain so we can read the chunks
		req.Header.Del("Accept-Encoding")
	}
	rp.ModifyResponse = func(resp *http.Response) error {
		path := resp.Request.URL.Path
		if path != "/api/generate" && path != "/api/chat" {
			return nil
		}
		resp.Body = &tap{rc: resp.Body, store: p.store, path: path}
		return nil
	}
	return rp
}

func (p *Proxy) Listen(addr string) error {
	return http.ListenAndServe(addr, p.Handler())
}

// chunk is the slice of an ollama response we care about. The final
// chunk (done:true) carries the counters; durations are nanoseconds.
type chunk struct {
	Model           string `json:"model"`
	Done            bool   `json:"done"`
	PromptEvalCount int    `json:"prompt_eval_count"`
	EvalCount       int    `json:"eval_count"`
	EvalDuration    int64  `json:"eval_duration"`
	TotalDuration   int64  `json:"total_duration"`
}

// tap reads through a (possibly streaming) ndjson body, recording the
// final chunk without buffering or delaying anything the client sees.
type tap struct {
	rc    io.ReadCloser
	store *Store
	path  string
	buf   bytes.Buffer
	done  bool
}

func (t *tap) Read(b []byte) (int, error) {
	n, err := t.rc.Read(b)
	if n > 0 && !t.done {
		t.buf.Write(b[:n])
		t.drain()
	}
	if err == io.EOF && !t.done {
		// non-streaming bodies may end without a trailing newline
		t.record(t.buf.Bytes())
	}
	return n, err
}

func (t *tap) drain() {
	for {
		line, err := t.buf.ReadBytes('\n')
		if err != nil {
			// partial line; buffer is empty now, so this re-queues it
			t.buf.Write(line)
			return
		}
		t.record(line)
	}
}

func (t *tap) record(line []byte) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return
	}
	var c chunk
	if json.Unmarshal(line, &c) != nil || !c.Done {
		return
	}
	t.done = true
	r := Request{
		When:     time.Now(),
		Path:     t.path,
		Model:    c.Model,
		PromptTk: c.PromptEvalCount,
		OutTk:    c.EvalCount,
		Total:    time.Duration(c.TotalDuration),
	}
	if c.EvalDuration > 0 {
		r.TokSec = float64(c.EvalCount) / (float64(c.EvalDuration) / 1e9)
	}
	t.store.Add(r)
}

func (t *tap) Close() error {
	if !t.done {
		t.record(t.buf.Bytes())
	}
	return t.rc.Close()
}
