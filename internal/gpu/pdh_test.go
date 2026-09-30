package gpu

import "testing"

func TestAggregatePDH(t *testing.T) {
	adapters := []pdhAdapter{{Name: "NVIDIA GeForce RTX 5090", TotalMiB: 32760}}

	tests := []struct {
		name string
		gfx  map[string]float64
		mem  map[string]float64
		want Stats
	}{
		{
			name: "3d and compute take the max, not the sum",
			gfx: map[string]float64{
				`pid_100_luid_0x00000000_0x0000ABCD_phys_0_eng_0_engtype_3D`:      40,
				`pid_100_luid_0x00000000_0x0000ABCD_phys_0_eng_1_engtype_Compute`: 65,
				`pid_100_luid_0x00000000_0x0000ABCD_phys_0_eng_2_engtype_Copy`:    90,
			},
			mem:  map[string]float64{`luid_0x00000000_0x0000ABCD_phys_0`: 2048 * (1 << 20)},
			want: Stats{Name: "NVIDIA GeForce RTX 5090", Util: 65, MemUsed: 2048, MemTotal: 32760, Temp: -1, Power: -1},
		},
		{
			name: "util caps at 100 across engines",
			gfx: map[string]float64{
				`pid_1_luid_0x0_0x1_phys_0_eng_0_engtype_3D`:      70,
				`pid_2_luid_0x0_0x1_phys_0_eng_0_engtype_Compute`: 130,
			},
			mem:  map[string]float64{`luid_0x0_0x1_phys_0`: 1024 * (1 << 20)},
			want: Stats{Name: "NVIDIA GeForce RTX 5090", Util: 100, MemUsed: 1024, MemTotal: 32760, Temp: -1, Power: -1},
		},
		{
			name: "no matching adapter falls back to bare GPU name",
			gfx:  map[string]float64{`pid_1_luid_0x0_0x2_phys_0_eng_0_engtype_3D`: 10},
			mem:  map[string]float64{`luid_0x0_0x2_phys_0`: 512 * (1 << 20)},
			want: Stats{Name: "GPU", Util: 10, MemUsed: 512, Temp: -1, Power: -1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []Stats
			if tt.name == "no matching adapter falls back to bare GPU name" {
				got = aggregatePDH(tt.gfx, tt.mem, nil)
			} else {
				got = aggregatePDH(tt.gfx, tt.mem, adapters)
			}
			if len(got) != 1 {
				t.Fatalf("expected 1 gpu, got %d: %+v", len(got), got)
			}
			if got[0] != tt.want {
				t.Fatalf("got %+v, want %+v", got[0], tt.want)
			}
		})
	}
}

func TestLuidOf(t *testing.T) {
	got := luidOf("pid_100_luid_0x00000000_0x0000abcd_phys_0_eng_0_engtype_3d")
	if got != "luid_0x00000000_0x0000abcd" {
		t.Fatalf("got %q", got)
	}
	if got := luidOf("no luid here"); got != "no luid here" {
		t.Fatalf("expected passthrough, got %q", got)
	}
}
