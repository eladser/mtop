package sources

import "fmt"

// llama-swap fronts llama.cpp and starts/stops whatever model a request
// asks for; /running lists everything it currently has spawned. It
// defaults to the same :8080 port as bare llama.cpp, so absent an
// explicit -llamaswap url this rides on -llamacpp instead.
func (s *Scanner) scanLlamaswap() ([]Row, bool) {
	base := s.llamaswap
	if base == "" {
		base = s.llamacpp
	}
	if base == "" {
		return nil, false
	}
	var resp struct {
		Running []struct {
			Model string `json:"model"`
			State string `json:"state"`
			TTL   int    `json:"ttl"`
		} `json:"running"`
	}
	if s.getJSON(base+"/running", &resp) != nil {
		return nil, false
	}
	var rows []Row
	for _, m := range resp.Running {
		m := m
		note := m.State
		if m.TTL > 0 {
			note = fmt.Sprintf("%s · ttl %ds", m.State, m.TTL)
		}
		rows = append(rows, Row{
			Name:   m.Model,
			Note:   note,
			From:   "llama-swap",
			Unload: func() error { return s.unloadLlamaswap(base, m.Model) },
		})
	}
	return rows, true
}

func (s *Scanner) unloadLlamaswap(base, model string) error {
	resp, err := s.hc.Post(base+"/api/models/unload/"+model, "application/json", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("llama-swap: %s", resp.Status)
	}
	return nil
}
