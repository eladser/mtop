package sources

import (
	"fmt"
	"path/filepath"
)

// SGLang's own metrics use a colon (sglang:token_usage); some deployments
// relabel or proxy them with an underscore instead, so read either.
func (s *Scanner) scanSglang() ([]Row, bool) {
	if s.sglang == "" {
		return nil, false
	}
	var info struct {
		ModelPath string `json:"model_path"`
	}
	if s.getJSON(s.sglang+"/get_model_info", &info) != nil {
		return nil, false
	}
	name := filepath.Base(info.ModelPath)
	if name == "." || name == "/" || name == "" {
		name = "(model)"
	}
	note := ""
	if m, err := s.getProm(s.sglang + "/metrics"); err == nil {
		kv := promVal(m, "sglang:token_usage", "sglang_token_usage")
		running := promVal(m, "sglang:num_running_reqs", "sglang_num_running_reqs")
		note = fmt.Sprintf("kv %.0f%% · %d running", kv*100, int(running))
	}
	return []Row{{Name: name, From: "sglang", Note: note}}, true
}

func promVal(m map[string]float64, names ...string) float64 {
	for _, n := range names {
		if v, ok := m[n]; ok {
			return v
		}
	}
	return 0
}
