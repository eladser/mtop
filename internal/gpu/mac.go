package gpu

import (
	"os/exec"
	"runtime"
	"strings"
)

// Apple Silicon has unified memory, so "GPU memory" is system memory.
// powermetrics has the real GPU numbers but macOS only lets root run
// it, so without sudo this degrades to a memory readout.
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
	return []Stats{g}, nil
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
