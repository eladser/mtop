package ollama

import (
	"encoding/json"
	"net/http"
	"time"
)

type Client struct {
	base string
	hc   *http.Client
}

func New(base string) *Client {
	return &Client{base: base, hc: &http.Client{Timeout: 3 * time.Second}}
}

type Model struct {
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	SizeVRAM  int64     `json:"size_vram"`
	ExpiresAt time.Time `json:"expires_at"`
	Details   Details   `json:"details"`
}

type Details struct {
	ParameterSize     string `json:"parameter_size"`
	QuantizationLevel string `json:"quantization_level"`
}

type listResponse struct {
	Models []Model `json:"models"`
}

// Loaded returns models currently in memory (what `ollama ps` shows).
func (c *Client) Loaded() ([]Model, error) {
	var r listResponse
	if err := c.get("/api/ps", &r); err != nil {
		return nil, err
	}
	return r.Models, nil
}

// OnDisk returns how many models are pulled locally.
func (c *Client) OnDisk() (int, error) {
	var r listResponse
	if err := c.get("/api/tags", &r); err != nil {
		return 0, err
	}
	return len(r.Models), nil
}

func (c *Client) get(path string, v any) error {
	resp, err := c.hc.Get(c.base + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(v)
}
