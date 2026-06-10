// Package sources finds models on whatever local AI servers happen to
// be running. Ollama is the main one; llama.cpp, LM Studio and vLLM
// get probed on their usual ports and merged into the same list.
package sources

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/eladser/mtop/internal/ollama"
)

type Row struct {
	Name    string
	Size    string
	Quant   string
	VRAM    int64
	Expires time.Time
	From    string
	Note    string       // extra per-server detail, e.g. kv-cache use
	Unload  func() error // nil when the server has no way to
}

type Scanner struct {
	oll      *ollama.Client
	llamacpp string
	lmstudio string
	vllm     string
	hc       *http.Client
}

func New(oll *ollama.Client, llamacpp, lmstudio, vllm string) *Scanner {
	return &Scanner{
		oll:      oll,
		llamacpp: llamacpp,
		lmstudio: lmstudio,
		vllm:     vllm,
		hc:       &http.Client{Timeout: 800 * time.Millisecond},
	}
}

// Scan returns every loaded model it can find, plus which servers
// answered. ollErr is ollama's error specifically, since that's the
// one worth telling the user about.
func (s *Scanner) Scan() (rows []Row, alive []string, ollErr error) {
	models, err := s.oll.Loaded()
	ollErr = err
	if err == nil {
		alive = append(alive, "ollama")
		for _, m := range models {
			m := m
			rows = append(rows, Row{
				Name:    m.Name,
				Size:    m.Details.ParameterSize,
				Quant:   m.Details.QuantizationLevel,
				VRAM:    m.SizeVRAM,
				Expires: m.ExpiresAt,
				From:    "ollama",
				Unload:  func() error { return s.oll.Unload(m.Name) },
			})
		}
	}
	if r, ok := s.scanLlamacpp(); ok {
		alive = append(alive, "llama.cpp")
		rows = append(rows, r...)
	}
	if r, ok := s.scanLMStudio(); ok {
		alive = append(alive, "lm studio")
		rows = append(rows, r...)
	}
	if r, ok := s.scanVllm(); ok {
		alive = append(alive, "vllm")
		rows = append(rows, r...)
	}
	return rows, alive, ollErr
}

// OnDisk is how many models ollama has pulled locally. Cosmetic, so
// errors just mean zero.
func (s *Scanner) OnDisk() int {
	n, _ := s.oll.OnDisk()
	return n
}

func (s *Scanner) getJSON(url string, v any) error {
	resp, err := s.hc.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(v)
}
