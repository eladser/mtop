package gpu

import "testing"

func TestParseAMDMetricsList(t *testing.T) {
	out := []byte(`[{"gpu":0,"usage":{"gfx_activity":15},"mem_usage":{"used_vram":1024,"total_vram":24560},"temperature":{"edge":45,"hotspot":52},"power":{"socket_power":63.5}}]`)
	metrics, err := parseAMDMetrics(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics) != 1 {
		t.Fatalf("expected 1 gpu, got %d", len(metrics))
	}
	m := metrics[0]
	if amdVal(m.Usage.GfxActivity) != 15 || amdVal(m.MemUsage.UsedVRAM) != 1024 ||
		amdVal(m.MemUsage.TotalVRAM) != 24560 || amdVal(m.Temperature.Edge) != 45 ||
		amdVal(m.Power.SocketPower) != 63.5 {
		t.Fatalf("bad parse: %+v", m)
	}
}

func TestParseAMDMetricsGpuData(t *testing.T) {
	out := []byte(`{"gpu_data":[{"gpu":0,"usage":{"gfx_activity":{"value":22,"unit":"%"}},"mem_usage":{"used_vram":{"value":2048,"unit":"MB"},"total_vram":{"value":24560,"unit":"MB"}},"temperature":{"edge":{"value":48,"unit":"C"},"hotspot":{"value":55,"unit":"C"}},"power":{"socket_power":{"value":70,"unit":"W"}}}]}`)
	metrics, err := parseAMDMetrics(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics) != 1 {
		t.Fatalf("expected 1 gpu, got %d", len(metrics))
	}
	m := metrics[0]
	if amdVal(m.Usage.GfxActivity) != 22 || amdVal(m.MemUsage.UsedVRAM) != 2048 ||
		amdVal(m.Temperature.Edge) != 48 || amdVal(m.Power.SocketPower) != 70 {
		t.Fatalf("bad parse: %+v", m)
	}
}

func TestParseAMDMetricsNA(t *testing.T) {
	out := []byte(`[{"gpu":0,"usage":{"gfx_activity":"N/A"},"mem_usage":{"used_vram":{"value":"N/A"},"total_vram":24560},"temperature":{"edge":"N/A","hotspot":{"value":51}},"power":{"socket_power":"N/A","average_socket_power":{"value":60.2}}}]`)
	metrics, err := parseAMDMetrics(out)
	if err != nil {
		t.Fatal(err)
	}
	m := metrics[0]
	if amdVal(m.Usage.GfxActivity) != -1 || amdVal(m.MemUsage.UsedVRAM) != -1 {
		t.Fatalf("expected -1 for N/A fields, got %+v", m)
	}
	temp := amdVal(m.Temperature.Edge)
	if temp >= 0 {
		t.Fatalf("edge should be N/A, got %v", temp)
	}
	if v := amdVal(m.Temperature.Hotspot); v != 51 {
		t.Fatalf("hotspot fallback: got %v", v)
	}
	if v := amdVal(m.Power.SocketPower); v >= 0 {
		t.Fatalf("socket_power should be N/A, got %v", v)
	}
	if v := amdVal(m.Power.AverageSocketPower); v != 60.2 {
		t.Fatalf("average_socket_power fallback: got %v", v)
	}
}

func TestParseAMDStaticShapes(t *testing.T) {
	list := []byte(`[{"gpu":0,"asic":{"market_name":"Radeon RX 7900 XTX"}}]`)
	s, err := parseAMDStatic(list)
	if err != nil || len(s) != 1 || s[0].Asic.MarketName != "Radeon RX 7900 XTX" {
		t.Fatalf("list shape: %+v, err %v", s, err)
	}

	wrapped := []byte(`{"gpu_data":[{"gpu":0,"asic":{"market_name":"Radeon RX 7900 XTX"}}]}`)
	s, err = parseAMDStatic(wrapped)
	if err != nil || len(s) != 1 || s[0].Asic.MarketName != "Radeon RX 7900 XTX" {
		t.Fatalf("gpu_data shape: %+v, err %v", s, err)
	}
}
