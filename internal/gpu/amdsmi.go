package gpu

import (
	"encoding/json"
	"os/exec"
	"strconv"
)

// amd-smi is rocm-smi's replacement; readAMDSMI is tried first and
// readAMD (amd.go) is the fallback for older ROCm installs that only
// ship rocm-smi. Both list ("[...]") and the newer ROCm 7
// {"gpu_data":[...]} wrapping are accepted, and every numeric field may
// come back as {"value":x}, a bare number, or "N/A".
type amdMetric struct {
	Gpu   int `json:"gpu"`
	Usage struct {
		GfxActivity json.RawMessage `json:"gfx_activity"`
	} `json:"usage"`
	MemUsage struct {
		UsedVRAM  json.RawMessage `json:"used_vram"`
		TotalVRAM json.RawMessage `json:"total_vram"`
	} `json:"mem_usage"`
	Temperature struct {
		Edge    json.RawMessage `json:"edge"`
		Hotspot json.RawMessage `json:"hotspot"`
	} `json:"temperature"`
	Power struct {
		SocketPower        json.RawMessage `json:"socket_power"`
		AverageSocketPower json.RawMessage `json:"average_socket_power"`
	} `json:"power"`
}

type amdStatic struct {
	Gpu  int `json:"gpu"`
	Asic struct {
		MarketName string `json:"market_name"`
	} `json:"asic"`
}

type amdSMI struct {
	path  string
	names map[int]string
}

func newAmdSMI(path string) *amdSMI { return &amdSMI{path: path} }

func (a *amdSMI) read() ([]Stats, error) {
	out, err := exec.Command(a.path, "metric", "-u", "-m", "-t", "-p", "--json").Output()
	if err != nil {
		return nil, err
	}
	metrics, err := parseAMDMetrics(out)
	if err != nil {
		return nil, err
	}
	if a.names == nil {
		a.names = a.loadNames()
	}
	var all []Stats
	for _, m := range metrics {
		g := Stats{Name: a.names[m.Gpu]}
		if g.Name == "" {
			g.Name = "AMD GPU"
		}
		g.Util = int(amdVal(m.Usage.GfxActivity))
		g.MemUsed = int(amdVal(m.MemUsage.UsedVRAM))
		g.MemTotal = int(amdVal(m.MemUsage.TotalVRAM))
		temp := amdVal(m.Temperature.Edge)
		if temp < 0 {
			temp = amdVal(m.Temperature.Hotspot)
		}
		g.Temp = int(temp)
		pwr := amdVal(m.Power.SocketPower)
		if pwr < 0 {
			pwr = amdVal(m.Power.AverageSocketPower)
		}
		g.Power = pwr
		all = append(all, g)
	}
	return all, nil
}

// loadNames is queried once and cached: the name doesn't change poll to
// poll and `static --json` is a separate, slower call.
func (a *amdSMI) loadNames() map[int]string {
	names := map[int]string{}
	out, err := exec.Command(a.path, "static", "--json").Output()
	if err != nil {
		return names
	}
	list, err := parseAMDStatic(out)
	if err != nil {
		return names
	}
	for _, s := range list {
		names[s.Gpu] = s.Asic.MarketName
	}
	return names
}

func parseAMDMetrics(out []byte) ([]amdMetric, error) {
	var wrapped struct {
		GpuData []amdMetric `json:"gpu_data"`
	}
	if err := json.Unmarshal(out, &wrapped); err == nil && wrapped.GpuData != nil {
		return wrapped.GpuData, nil
	}
	var list []amdMetric
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, err
	}
	return list, nil
}

func parseAMDStatic(out []byte) ([]amdStatic, error) {
	var wrapped struct {
		GpuData []amdStatic `json:"gpu_data"`
	}
	if err := json.Unmarshal(out, &wrapped); err == nil && wrapped.GpuData != nil {
		return wrapped.GpuData, nil
	}
	var list []amdStatic
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// amdVal unwraps amd-smi's {"value":x} shape, a bare number, or "N/A"
// (either bare or nested inside value) into a float, -1 for unknown.
func amdVal(raw json.RawMessage) float64 {
	if len(raw) == 0 {
		return -1
	}
	var wrapped struct {
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && wrapped.Value != nil {
		raw = wrapped.Value
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if s == "N/A" {
			return -1
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return -1
		}
		return v
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err != nil {
		return -1
	}
	return f
}
