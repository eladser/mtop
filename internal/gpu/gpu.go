// Package gpu reads NVIDIA stats by polling nvidia-smi's query
// interface. No cgo, works wherever the driver is installed.
// Native NVML bindings and AMD/Apple come later (docs/roadmap.md).
package gpu

import (
	"os/exec"
	"strconv"
	"strings"
)

type Stats struct {
	Name     string
	Util     int     // %
	MemUsed  int     // MiB
	MemTotal int     // MiB
	Temp     int     // celsius
	Power    float64 // watts
}

type Reader struct {
	nvidia string
	amd    string
	mac    bool
}

func New() *Reader {
	r := &Reader{mac: macSupported()}
	if p, err := exec.LookPath("nvidia-smi"); err == nil {
		r.nvidia = p
	}
	if p, err := exec.LookPath("rocm-smi"); err == nil {
		r.amd = p
	}
	return r
}

func (r *Reader) Available() bool { return r.nvidia != "" || r.amd != "" || r.mac }

func (r *Reader) Read() ([]Stats, error) {
	var all []Stats
	var lastErr error
	if r.nvidia != "" {
		s, err := readNvidia(r.nvidia)
		all, lastErr = append(all, s...), err
	}
	if r.amd != "" {
		s, err := readAMD(r.amd)
		all, lastErr = append(all, s...), err
	}
	if r.mac {
		s, err := readMac()
		all, lastErr = append(all, s...), err
	}
	if len(all) > 0 {
		return all, nil
	}
	return nil, lastErr
}

func readNvidia(path string) ([]Stats, error) {
	out, err := exec.Command(path,
		"--query-gpu=name,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw",
		"--format=csv,noheader,nounits").Output()
	if err != nil {
		return nil, err
	}
	return parse(string(out)), nil
}

func parse(out string) []Stats {
	var all []Stats
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Split(line, ", ")
		if len(f) < 6 {
			continue
		}
		all = append(all, Stats{
			Name:     strings.TrimSpace(f[0]),
			Util:     atoi(f[1]),
			MemUsed:  atoi(f[2]),
			MemTotal: atoi(f[3]),
			Temp:     atoi(f[4]),
			Power:    atof(f[5]),
		})
	}
	return all
}

// nvidia-smi prints "[N/A]" for a field the card/driver doesn't support
// (e.g. power draw on older cards); -1 marks that as unknown rather than
// a real zero.
func atoi(s string) int {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "N/A") {
		return -1
	}
	n, _ := strconv.Atoi(s)
	return n
}

func atof(s string) float64 {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "N/A") {
		return -1
	}
	v, _ := strconv.ParseFloat(s, 64)
	return v
}
