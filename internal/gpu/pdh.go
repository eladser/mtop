package gpu

import (
	"sort"
	"strings"
)

// pdhAdapter is a display adapter read from the registry: name and total
// VRAM, since PDH itself doesn't expose either.
type pdhAdapter struct {
	Name     string
	TotalMiB int
}

// aggregatePDH turns raw \GPU Engine and \GPU Adapter Memory counter
// instances into per-GPU Stats. Instance names carry the luid, e.g.
// "pid_1234_luid_0x00000000_0x0000abcd_phys_0_eng_0_engtype_3d"; group
// by luid, take the max of the 3D and Compute engines rather than
// summing them (running both at once isn't double the load), then pair
// each luid with a registry adapter by rank: largest adapter with the
// highest VRAM usage. That last part is a heuristic, not a real
// luid-to-adapter lookup, and it's the one place multi-GPU boxes can end
// up mismatched.
func aggregatePDH(gfx, mem map[string]float64, adapters []pdhAdapter) []Stats {
	util := map[string]float64{}
	for inst, v := range gfx {
		l := strings.ToLower(inst)
		if !strings.Contains(l, "engtype_3d") && !strings.Contains(l, "engtype_compute") {
			continue
		}
		luid := luidOf(l)
		if v > util[luid] {
			util[luid] = v
		}
	}

	used := map[string]float64{}
	for inst, v := range mem {
		luid := luidOf(strings.ToLower(inst))
		used[luid] += v
	}

	type row struct {
		luid string
		mb   float64
	}
	rows := make([]row, 0, len(used))
	for luid, v := range used {
		rows = append(rows, row{luid, v / (1 << 20)})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].mb > rows[j].mb })

	all := make([]Stats, 0, len(rows))
	for i, r := range rows {
		g := Stats{
			Name:    "GPU",
			Util:    capUtil(util[r.luid]),
			MemUsed: int(r.mb),
			Temp:    -1,
			Power:   -1,
		}
		if i < len(adapters) {
			g.Name = adapters[i].Name
			g.MemTotal = adapters[i].TotalMiB
		}
		all = append(all, g)
	}
	return all
}

func luidOf(inst string) string {
	i := strings.Index(inst, "luid_")
	if i < 0 {
		return inst
	}
	rest := inst[i:]
	if j := strings.Index(rest, "_phys"); j >= 0 {
		return rest[:j]
	}
	return rest
}

func capUtil(v float64) int {
	if v > 100 {
		v = 100
	}
	return int(v + 0.5)
}
