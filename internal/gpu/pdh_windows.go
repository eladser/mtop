//go:build windows

package gpu

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// PDH via lazy DLL calls, no cgo. Used only when neither nvidia-smi nor
// amd-smi/rocm-smi is on PATH. Get-Counter spawns a PowerShell process
// per poll and typeperf has localized counter names and frozen
// instances, so both were ruled out in favor of this.
var (
	pdhDLL                  = windows.NewLazySystemDLL("pdh.dll")
	procPdhOpenQuery        = pdhDLL.NewProc("PdhOpenQueryW")
	procPdhAddEnglishCtr    = pdhDLL.NewProc("PdhAddEnglishCounterW")
	procPdhRemoveCounter    = pdhDLL.NewProc("PdhRemoveCounter")
	procPdhCollectQueryData = pdhDLL.NewProc("PdhCollectQueryData")
	procPdhGetCounterArray  = pdhDLL.NewProc("PdhGetFormattedCounterArrayW")
	procPdhCloseQuery       = pdhDLL.NewProc("PdhCloseQuery")
)

const pdhFmtDouble = 0x00000200

type pdhCounterValue struct {
	CStatus uint32
	Value   float64
}

type pdhCounterItem struct {
	Name  *uint16
	Value pdhCounterValue
}

type pdhReader struct {
	query uintptr
	gfx   uintptr
	mem   uintptr

	adapters []pdhAdapter
	added    time.Time
}

func newPDHReader() *pdhReader {
	if err := pdhDLL.Load(); err != nil {
		return nil
	}
	p := &pdhReader{adapters: readAdapters()}
	var q uintptr
	if r, _, _ := procPdhOpenQuery.Call(0, 0, uintptr(unsafe.Pointer(&q))); r != 0 {
		return nil
	}
	p.query = q
	if err := p.addCounters(); err != nil {
		procPdhCloseQuery.Call(p.query)
		return nil
	}
	// first collect just primes the counters; the GPU Engine wildcard
	// needs one sample before instances resolve.
	procPdhCollectQueryData.Call(p.query)
	return p
}

func (p *pdhReader) addCounters() error {
	gfxPath, _ := windows.UTF16PtrFromString(`\GPU Engine(*)\Utilization Percentage`)
	memPath, _ := windows.UTF16PtrFromString(`\GPU Adapter Memory(*)\Dedicated Usage`)
	var gfx, mem uintptr
	if r, _, _ := procPdhAddEnglishCtr.Call(p.query, uintptr(unsafe.Pointer(gfxPath)), 0, uintptr(unsafe.Pointer(&gfx))); r != 0 {
		return fmt.Errorf("pdh: add gfx counter: %#x", r)
	}
	if r, _, _ := procPdhAddEnglishCtr.Call(p.query, uintptr(unsafe.Pointer(memPath)), 0, uintptr(unsafe.Pointer(&mem))); r != 0 {
		return fmt.Errorf("pdh: add mem counter: %#x", r)
	}
	p.gfx, p.mem = gfx, mem
	p.added = time.Now()
	return nil
}

// GPU Engine instances are per-process and come and go as processes
// start GPU work; a wildcard counter only expands once at add time, so
// it's removed and re-added periodically to pick up new ones.
func (p *pdhReader) readd() {
	procPdhRemoveCounter.Call(p.gfx)
	procPdhRemoveCounter.Call(p.mem)
	p.addCounters()
}

func (p *pdhReader) read() ([]Stats, error) {
	if p == nil || p.query == 0 {
		return nil, fmt.Errorf("pdh: not initialized")
	}
	if time.Since(p.added) > 30*time.Second {
		p.readd()
	}
	if r, _, _ := procPdhCollectQueryData.Call(p.query); r != 0 {
		return nil, fmt.Errorf("pdh: PdhCollectQueryData: %#x", r)
	}
	gfx, err := readCounterArray(p.gfx)
	if err != nil {
		return nil, err
	}
	mem, err := readCounterArray(p.mem)
	if err != nil {
		return nil, err
	}
	return aggregatePDH(gfx, mem, p.adapters), nil
}

func readCounterArray(counter uintptr) (map[string]float64, error) {
	var size, count uint32
	procPdhGetCounterArray.Call(counter, pdhFmtDouble, uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&count)), 0)
	if size == 0 || count == 0 {
		return map[string]float64{}, nil
	}
	buf := make([]byte, size)
	if r, _, _ := procPdhGetCounterArray.Call(counter, pdhFmtDouble, uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&buf[0]))); r != 0 {
		return nil, fmt.Errorf("pdh: PdhGetFormattedCounterArray: %#x", r)
	}
	items := unsafe.Slice((*pdhCounterItem)(unsafe.Pointer(&buf[0])), count)
	out := make(map[string]float64, count)
	for _, it := range items {
		if it.Value.CStatus != 0 || it.Name == nil {
			continue
		}
		out[windows.UTF16PtrToString(it.Name)] = it.Value.Value
	}
	return out, nil
}

// readAdapters lists display adapters under the class registry key for
// their driver-reported name and dedicated VRAM size, since PDH itself
// has no notion of either.
func readAdapters() []pdhAdapter {
	const classKey = `SYSTEM\CurrentControlSet\Control\Class\{4d36e968-e325-11ce-bfc1-08002be10318}`
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, classKey, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return nil
	}
	defer k.Close()
	subs, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return nil
	}
	var out []pdhAdapter
	for _, sub := range subs {
		if _, err := strconv.Atoi(sub); err != nil {
			continue // skip "Properties" and the like, adapters are "0000", "0001", ...
		}
		sk, err := registry.OpenKey(registry.LOCAL_MACHINE, classKey+`\`+sub, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		desc, _, descErr := sk.GetStringValue("DriverDesc")
		size, _, sizeErr := sk.GetIntegerValue("HardwareInformation.qwMemorySize")
		sk.Close()
		if descErr != nil || sizeErr != nil {
			continue
		}
		out = append(out, pdhAdapter{Name: strings.TrimSpace(desc), TotalMiB: int(size / (1 << 20))})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TotalMiB > out[j].TotalMiB })
	return out
}
