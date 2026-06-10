package sources

import (
	"fmt"
	"path/filepath"
)

// llama.cpp server: /props has the loaded model, /metrics (needs
// --metrics on launch) has kv-cache use and how many requests are in
// flight right now.
func (s *Scanner) scanLlamacpp() ([]Row, bool) {
	if s.llamacpp == "" {
		return nil, false
	}
	var props struct {
		ModelPath string `json:"model_path"`
		Settings  struct {
			NCtx int `json:"n_ctx"`
		} `json:"default_generation_settings"`
	}
	if s.getJSON(s.llamacpp+"/props", &props) != nil {
		return nil, false
	}
	name := filepath.Base(props.ModelPath)
	if name == "." || name == "/" {
		name = "(model)"
	}
	note := ""
	if m, err := s.getProm(s.llamacpp + "/metrics"); err == nil {
		note = fmt.Sprintf("kv %.0f%% · %d running",
			m["llamacpp:kv_cache_usage_ratio"]*100, int(m["llamacpp:requests_processing"]))
	}
	row := Row{Name: name, From: "llama.cpp", Note: note}
	if props.Settings.NCtx > 0 {
		row.Size = fmt.Sprintf("%dk ctx", props.Settings.NCtx/1024)
	}
	return []Row{row}, true
}
