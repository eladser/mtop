package sources

// LM Studio 0.4+ speaks a v1 REST api where models carry a
// loaded_instances list instead of a state field. Older LM Studio only
// has v0, where "loaded" is a plain string field.
func (s *Scanner) scanLMStudio() ([]Row, bool) {
	if s.lmstudio == "" {
		return nil, false
	}
	if rows, ok := s.scanLMStudioV1(); ok {
		return rows, true
	}
	return s.scanLMStudioV0()
}

func (s *Scanner) scanLMStudioV1() ([]Row, bool) {
	var resp struct {
		Models []struct {
			Key          string `json:"key"`
			Quantization struct {
				Name string `json:"name"`
			} `json:"quantization"`
			LoadedInstances []struct {
				ID string `json:"id"`
			} `json:"loaded_instances"`
		} `json:"models"`
	}
	if s.getJSON(s.lmstudio+"/api/v1/models", &resp) != nil {
		return nil, false
	}
	var rows []Row
	for _, m := range resp.Models {
		for _, inst := range m.LoadedInstances {
			name := inst.ID
			if name == "" {
				name = m.Key
			}
			rows = append(rows, Row{Name: name, Quant: m.Quantization.Name, From: "lm studio"})
		}
	}
	return rows, true
}

func (s *Scanner) scanLMStudioV0() ([]Row, bool) {
	var resp struct {
		Data []struct {
			ID           string `json:"id"`
			State        string `json:"state"`
			Quantization string `json:"quantization"`
			MaxContext   int    `json:"max_context_length"`
		} `json:"data"`
	}
	if s.getJSON(s.lmstudio+"/api/v0/models", &resp) != nil {
		return nil, false
	}
	var rows []Row
	for _, m := range resp.Data {
		if m.State != "loaded" {
			continue
		}
		rows = append(rows, Row{Name: m.ID, Quant: m.Quantization, From: "lm studio"})
	}
	return rows, true
}
