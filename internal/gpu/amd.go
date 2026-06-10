package gpu

import (
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
)

// AMD via rocm-smi's json output. The keys are long prose strings and
// have moved around between versions, so match loosely.
func readAMD(path string) ([]Stats, error) {
	out, err := exec.Command(path, "--showproductname", "--showuse", "--showmeminfo", "vram", "--showtemp", "--showpower", "--json").Output()
	if err != nil {
		return nil, err
	}
	return parseAMD(out), nil
}

func parseAMD(out []byte) []Stats {
	var cards map[string]map[string]string
	if json.Unmarshal(out, &cards) != nil {
		return nil
	}
	var all []Stats
	for card, kv := range cards {
		if !strings.HasPrefix(card, "card") {
			continue
		}
		g := Stats{Name: "AMD GPU"}
		for k, v := range kv {
			switch {
			case strings.Contains(k, "Card series") || strings.Contains(k, "Card Series"):
				g.Name = v
			case strings.Contains(k, "GPU use"):
				g.Util = atoi(v)
			case strings.Contains(k, "Temperature") && strings.Contains(k, "edge"):
				g.Temp = int(atof(v))
			case strings.Contains(k, "Power") && strings.Contains(k, "W"):
				g.Power = atof(v)
			case strings.Contains(k, "VRAM Total Memory"):
				g.MemTotal = int(parseBytes(v) / (1 << 20))
			case strings.Contains(k, "VRAM Total Used"):
				g.MemUsed = int(parseBytes(v) / (1 << 20))
			}
		}
		all = append(all, g)
	}
	return all
}

func parseBytes(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}
