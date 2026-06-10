package sources

import (
	"bufio"
	"io"
	"strconv"
	"strings"
)

// Just enough prometheus text parsing for what llama.cpp and vLLM
// expose. Values with the same name get summed across label sets,
// which is the right thing for the counters we read.
type promData struct {
	vals   map[string]float64
	labels map[string]string // first seen value per label key
}

func (p promData) label(key string) string { return p.labels[key] }

func parseProm(body string) promData {
	p := promData{vals: map[string]float64{}, labels: map[string]string{}}
	sc := bufio.NewScanner(strings.NewReader(body))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		sp := strings.LastIndexByte(line, ' ')
		if sp < 0 {
			continue
		}
		v, err := strconv.ParseFloat(line[sp+1:], 64)
		if err != nil {
			continue
		}
		name := line[:sp]
		if b := strings.IndexByte(name, '{'); b >= 0 {
			parseLabels(name[b+1:], p.labels)
			name = name[:b]
		}
		p.vals[name] += v
	}
	return p
}

func parseLabels(s string, into map[string]string) {
	for _, part := range strings.Split(strings.TrimSuffix(s, "}"), ",") {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if _, seen := into[k]; !seen {
			into[k] = strings.Trim(v, `"`)
		}
	}
}

func (s *Scanner) getProm(url string) (map[string]float64, error) {
	p, err := s.getPromLabeled(url)
	if err != nil {
		return nil, err
	}
	return p.vals, nil
}

func (s *Scanner) getPromLabeled(url string) (promData, error) {
	resp, err := s.hc.Get(url)
	if err != nil {
		return promData{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return promData{}, err
	}
	return parseProm(string(body)), nil
}
