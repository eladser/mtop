package gpu

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// Apple Silicon shares one pool of memory between cpu and gpu, so the
// "gpu memory" here is really system memory. Utilization is the catch:
// powermetrics has it but only runs as root, so we only reach for it
// when mtop itself is root. Otherwise the util column stays blank and
// you still get the memory figure.
func readMac() ([]Stats, error) {
	total, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
	if err != nil {
		return nil, err
	}
	g := Stats{
		Name:     "Apple Silicon (unified memory)",
		MemTotal: int(parseBytes(strings.TrimSpace(string(total))) / (1 << 20)),
	}
	if out, err := exec.Command("vm_stat").Output(); err == nil {
		g.MemUsed = usedFromVMStat(string(out))
	}
	if os.Geteuid() == 0 {
		if u, ok := gpuResidency(); ok {
			g.Util = u
		}
	}
	return []Stats{g}, nil
}

// one short powermetrics sample, pull the gpu active residency out of it
func gpuResidency() (int, bool) {
	out, err := exec.Command("powermetrics", "--samplers", "gpu_power", "-n", "1", "-i", "200").Output()
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if v, ok := residencyOf(line); ok {
			return v, true
		}
	}
	return 0, false
}

// residencyOf reads "GPU HW active residency:  42.50% ..." into 43.
func residencyOf(line string) (int, bool) {
	if !strings.Contains(line, "active residency") {
		return 0, false
	}
	i := strings.IndexByte(line, ':')
	if i < 0 {
		return 0, false
	}
	f := strings.Fields(line[i+1:])
	if len(f) == 0 {
		return 0, false
	}
	v, err := strconv.ParseFloat(strings.TrimSuffix(f[0], "%"), 64)
	if err != nil {
		return 0, false
	}
	return int(v + 0.5), true
}

func usedFromVMStat(out string) int {
	pageSize := int64(16384)
	var used int64
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "page size of") {
			f := strings.Fields(line)
			for i, w := range f {
				if w == "of" && i+1 < len(f) {
					pageSize = parseBytes(f[i+1])
				}
			}
		}
		for _, key := range []string{"Pages active", "Pages wired down", "Pages occupied by compressor"} {
			if strings.HasPrefix(line, key) {
				v := strings.TrimSpace(strings.TrimSuffix(line[strings.IndexByte(line, ':')+1:], "."))
				used += parseBytes(v)
			}
		}
	}
	return int(used * pageSize / (1 << 20))
}

func macSupported() bool { return runtime.GOOS == "darwin" }
