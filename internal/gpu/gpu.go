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
	path string
}

func New() *Reader {
	p, err := exec.LookPath("nvidia-smi")
	if err != nil {
		return &Reader{}
	}
	return &Reader{path: p}
}

func (r *Reader) Available() bool { return r.path != "" }

func (r *Reader) Read() ([]Stats, error) {
	out, err := exec.Command(r.path,
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

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func atof(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v
}
