// Package compare runs the same prompt against a few models and reports
// how each one did. Backs the `mtop compare` subcommand.
package compare

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"
)

type Result struct {
	Model  string
	OutTk  int
	TokSec float64
	Total  time.Duration
	Err    error
}

// Run sends prompt to each model on base (an ollama url), one at a time
// so they aren't fighting over the GPU, and times the replies.
func Run(base, prompt string, models []string) []Result {
	out := make([]Result, len(models))
	for i, m := range models {
		out[i] = one(base, prompt, m)
	}
	return out
}

// RunOpenAI is the same idea against an OpenAI-style endpoint (llama.cpp,
// LM Studio, vLLM). Those carry no generation timings, so tok/s here is
// tokens over wall-clock, prompt processing included.
func RunOpenAI(base, prompt string, models []string) []Result {
	out := make([]Result, len(models))
	for i, m := range models {
		out[i] = oneOpenAI(base, prompt, m)
	}
	return out
}

func oneOpenAI(base, prompt, model string) Result {
	r := Result{Model: model}
	body, _ := json.Marshal(map[string]any{
		"model":    model,
		"messages": []map[string]string{{"role": "user", "content": prompt}},
		"stream":   false,
	})
	start := time.Now()
	resp, err := http.Post(base+"/chat/completions", "application/json", bytes.NewReader(body))
	if err != nil {
		r.Err = err
		return r
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		r.Err = fmt.Errorf("%s", resp.Status)
		return r
	}
	var d struct {
		Usage struct {
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		r.Err = err
		return r
	}
	r.OutTk = d.Usage.CompletionTokens
	r.Total = time.Since(start)
	if s := r.Total.Seconds(); s > 0 {
		r.TokSec = float64(r.OutTk) / s
	}
	return r
}

func one(base, prompt, model string) Result {
	r := Result{Model: model}
	body, _ := json.Marshal(map[string]any{"model": model, "prompt": prompt, "stream": false})
	resp, err := http.Post(base+"/api/generate", "application/json", bytes.NewReader(body))
	if err != nil {
		r.Err = err
		return r
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		r.Err = fmt.Errorf("%s", resp.Status)
		return r
	}
	var d struct {
		EvalCount     int   `json:"eval_count"`
		EvalDuration  int64 `json:"eval_duration"`
		TotalDuration int64 `json:"total_duration"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		r.Err = err
		return r
	}
	r.OutTk = d.EvalCount
	r.Total = time.Duration(d.TotalDuration)
	if d.EvalDuration > 0 {
		r.TokSec = float64(d.EvalCount) / (float64(d.EvalDuration) / 1e9)
	}
	return r
}

// Table renders the results as aligned text, fastest first. Anything
// that errored sorts to the bottom (tok/s 0).
func Table(rs []Result) string {
	rs = append([]Result(nil), rs...)
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].TokSec > rs[j].TokSec })
	var b bytes.Buffer
	fmt.Fprintf(&b, "%-24s %10s %8s %10s\n", "MODEL", "TOK/S", "OUT", "TOTAL")
	for _, r := range rs {
		if r.Err != nil {
			fmt.Fprintf(&b, "%-24s %s\n", r.Model, r.Err)
			continue
		}
		fmt.Fprintf(&b, "%-24s %10.1f %8d %10s\n", r.Model, r.TokSec, r.OutTk, r.Total.Round(time.Millisecond))
	}
	return b.String()
}
