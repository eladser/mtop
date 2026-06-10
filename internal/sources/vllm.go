package sources

import "fmt"

// vLLM only talks prometheus. The model name rides along as a label.
func (s *Scanner) scanVllm() ([]Row, bool) {
	if s.vllm == "" {
		return nil, false
	}
	m, err := s.getPromLabeled(s.vllm + "/metrics")
	if err != nil {
		return nil, false
	}
	name := m.label("model_name")
	if name == "" {
		name = "(model)"
	}
	note := fmt.Sprintf("cache %.0f%% · %d running",
		m.vals["vllm:gpu_cache_usage_perc"]*100, int(m.vals["vllm:num_requests_running"]))
	return []Row{{Name: name, From: "vllm", Note: note}}, true
}
