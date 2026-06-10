package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/eladser/mtop/internal/gpu"
	"github.com/eladser/mtop/internal/proxy"
	"github.com/eladser/mtop/internal/sources"
)

const pollEvery = 1500 * time.Millisecond

var (
	accent  = lipgloss.Color("#4ECDC4")
	warn    = lipgloss.Color("#FF6B6B")
	dim     = lipgloss.Color("240")
	titleSt = lipgloss.NewStyle().Foreground(accent).Bold(true)
	dimSt   = lipgloss.NewStyle().Foreground(dim)
	warnSt  = lipgloss.NewStyle().Foreground(warn)
	selSt   = lipgloss.NewStyle().Foreground(accent)
	paneSt  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(dim).Padding(0, 1)
)

type App struct {
	scan      *sources.Scanner
	gpu       *gpu.Reader
	store     *proxy.Store
	listen    string
	version   string
	idleAfter time.Duration

	w, h    int
	sel     int
	byModel bool
	rows    []sources.Row
	alive   []string
	disk    int
	ollErr  error
	gpus    []gpu.Stats
	gpuErr  error

	// when mtop first saw each model loaded, for idle tracking
	seen    map[string]time.Time
	flash   string
	flashAt time.Time
	flashOk bool
}

func New(scan *sources.Scanner, g *gpu.Reader, store *proxy.Store, listen, version string, idleAfter time.Duration) *App {
	return &App{scan: scan, gpu: g, store: store, listen: listen, version: version,
		idleAfter: idleAfter, seen: map[string]time.Time{}}
}

type tick struct{}

type data struct {
	rows   []sources.Row
	alive  []string
	disk   int
	ollErr error
	gpus   []gpu.Stats
	gpuErr error
}

type unloaded struct {
	model string
	err   error
	auto  bool
}

func (a *App) Init() tea.Cmd { return a.poll }

func (a *App) poll() tea.Msg {
	var d data
	d.rows, d.alive, d.ollErr = a.scan.Scan()
	if d.ollErr == nil {
		d.disk = a.scan.OnDisk()
	}
	if a.gpu.Available() {
		d.gpus, d.gpuErr = a.gpu.Read()
	}
	return d
}

func (a *App) unload(row sources.Row, auto bool) tea.Cmd {
	return func() tea.Msg {
		return unloaded{model: row.Name, err: row.Unload(), auto: auto}
	}
}

// idle finds a model that hasn't served a proxied request (and that
// mtop has watched) for longer than idleAfter.
func (a *App) idle() (sources.Row, bool) {
	if a.idleAfter <= 0 {
		return sources.Row{}, false
	}
	for _, r := range a.rows {
		if r.Unload == nil {
			continue
		}
		ref := a.seen[r.Name]
		if last := a.store.LastSeen(r.Name); last.After(ref) {
			ref = last
		}
		if !ref.IsZero() && time.Since(ref) > a.idleAfter {
			return r, true
		}
	}
	return sources.Row{}, false
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.w, a.h = m.Width, m.Height
	case tea.KeyMsg:
		switch m.String() {
		case "q", "ctrl+c":
			return a, tea.Quit
		case "up", "k":
			if a.sel > 0 {
				a.sel--
			}
		case "down", "j":
			if a.sel < len(a.rows)-1 {
				a.sel++
			}
		case "c":
			a.byModel = !a.byModel
		case "u":
			if a.sel < len(a.rows) {
				row := a.rows[a.sel]
				if row.Unload == nil {
					a.flash, a.flashAt, a.flashOk = row.From+" has no unload api", time.Now(), false
					return a, nil
				}
				return a, a.unload(row, false)
			}
		}
	case data:
		a.rows, a.alive, a.disk, a.ollErr = m.rows, m.alive, m.disk, m.ollErr
		a.gpus, a.gpuErr = m.gpus, m.gpuErr
		if a.sel >= len(a.rows) {
			a.sel = max(0, len(a.rows)-1)
		}
		now := time.Now()
		current := map[string]bool{}
		for _, r := range a.rows {
			current[r.Name] = true
			if _, ok := a.seen[r.Name]; !ok {
				a.seen[r.Name] = now
			}
		}
		for name := range a.seen {
			if !current[name] {
				delete(a.seen, name)
			}
		}
		next := tea.Tick(pollEvery, func(time.Time) tea.Msg { return tick{} })
		if row, ok := a.idle(); ok {
			delete(a.seen, row.Name) // don't fire again while the unload is in flight
			return a, tea.Batch(next, a.unload(row, true))
		}
		return a, next
	case tick:
		return a, a.poll
	case unloaded:
		a.flashAt = time.Now()
		a.flashOk = m.err == nil
		switch {
		case m.err != nil:
			a.flash = fmt.Sprintf("couldn't unload %s: %v", m.model, m.err)
		case m.auto:
			a.flash = fmt.Sprintf("auto-unloaded %s (idle %s)", m.model, a.idleAfter)
		default:
			a.flash = "unloaded " + m.model
		}
		return a, a.poll
	}
	return a, nil
}

func (a *App) View() string {
	if a.w == 0 {
		return "starting..."
	}
	paneW := (a.w - 6) / 2
	top := lipgloss.JoinHorizontal(lipgloss.Top,
		paneSt.Width(paneW).Render(a.modelsPane()),
		paneSt.Width(paneW).Render(a.gpuPane()),
	)
	var mid string
	if a.byModel {
		mid = paneSt.Width(a.w - 4).Render(a.byModelPane())
	} else {
		mid = paneSt.Width(a.w - 4).Render(a.requestsPane())
	}
	spark := paneSt.Width(a.w - 4).Render(a.throughputPane())
	return lipgloss.JoinVertical(lipgloss.Left, top, mid, spark, a.statusLine())
}

// how many request rows fit without pushing the status line off screen
func (a *App) reqRows() int {
	topLines := max(len(a.rows)+2, len(a.gpus)*2+1) // header+rows vs name+stats per gpu
	free := a.h - topLines - 13
	return min(max(free, 4), 30)
}

func (a *App) statusLine() string {
	if alert := a.gpuAlert(); alert != "" {
		return warnSt.Render("  " + alert)
	}
	help := fmt.Sprintf("  ↑/↓ select · u unload · c %s · q quit · proxy on %s · v%s",
		map[bool]string{true: "requests", false: "by model"}[a.byModel], a.listen, a.version)
	if a.flash != "" && time.Since(a.flashAt) < 12*time.Second {
		st := selSt
		if !a.flashOk {
			st = warnSt
		}
		return st.Render("  "+a.flash) + dimSt.Render(" ·"+help[2:])
	}
	return dimSt.Render(help)
}

func (a *App) gpuAlert() string {
	for _, g := range a.gpus {
		if g.MemTotal > 0 && g.MemUsed*100/g.MemTotal >= 93 {
			return fmt.Sprintf("%s memory is at %d%% — press u on what you can spare", g.Name, g.MemUsed*100/g.MemTotal)
		}
		if g.Temp >= 87 {
			return fmt.Sprintf("%s is at %d°C", g.Name, g.Temp)
		}
	}
	return ""
}

func (a *App) modelsPane() string {
	var b strings.Builder
	b.WriteString(titleSt.Render("MODELS"))
	if len(a.alive) > 1 {
		b.WriteString(dimSt.Render("  " + strings.Join(a.alive, " · ")))
	}
	if a.disk > 0 {
		b.WriteString(dimSt.Render(fmt.Sprintf("  %d on disk", a.disk)))
	}
	multi := len(a.alive) > 1
	switch {
	case a.ollErr != nil && len(a.rows) == 0:
		b.WriteString("\n" + dimSt.Render("ollama not reachable — is it running?"))
	case len(a.rows) == 0:
		b.WriteString("\n" + dimSt.Render("nothing loaded — run a model and it shows up here"))
	default:
		head := fmt.Sprintf("  %-22s %8s %6s %6s  %s", "NAME", "SIZE", "QUANT", "VRAM", "TTL")
		if multi {
			head = fmt.Sprintf("  %-22s %-9s %8s %6s %6s  %s", "NAME", "FROM", "SIZE", "QUANT", "VRAM", "TTL")
		}
		b.WriteString("\n" + dimSt.Render(head))
		for i, r := range a.rows {
			line := a.modelLine(r, multi)
			if i == a.sel {
				b.WriteString("\n" + selSt.Render("▸ "+line))
			} else {
				b.WriteString("\n  " + line)
			}
		}
	}
	return b.String()
}

func (a *App) modelLine(r sources.Row, multi bool) string {
	vram := "—"
	if r.VRAM > 0 {
		vram = gib(r.VRAM)
	}
	ttl := ttlFor(r)
	if multi {
		return fmt.Sprintf("%-22s %-9s %8s %6s %6s  %s", r.Name, r.From, r.Size, r.Quant, vram, ttl)
	}
	return fmt.Sprintf("%-22s %8s %6s %6s  %s", r.Name, r.Size, r.Quant, vram, ttl)
}

func ttlFor(r sources.Row) string {
	switch {
	case overdue(r):
		return warnSt.Render("overdue — press u")
	case r.Expires.IsZero() && r.Note != "":
		return dimSt.Render(r.Note)
	case r.Expires.IsZero():
		return "—"
	}
	return time.Until(r.Expires).Round(time.Second).String()
}

func overdue(r sources.Row) bool {
	return !r.Expires.IsZero() && time.Now().After(r.Expires)
}

func (a *App) gpuPane() string {
	var b strings.Builder
	b.WriteString(titleSt.Render("GPU"))
	switch {
	case !a.gpu.Available():
		b.WriteString("\n" + dimSt.Render("no gpu tool found (nvidia-smi / rocm-smi)"))
	case a.gpuErr != nil:
		b.WriteString("\n" + dimSt.Render("gpu read failed: "+a.gpuErr.Error()))
	default:
		for _, g := range a.gpus {
			pct := 0
			if g.MemTotal > 0 {
				pct = g.MemUsed * 100 / g.MemTotal
			}
			st := lipgloss.NewStyle()
			if pct >= 93 || g.Temp >= 87 {
				st = warnSt
			}
			b.WriteString("\n" + g.Name + "\n" + st.Render(fmt.Sprintf(
				"util %3d%%  mem %d/%d MiB (%d%%)  %d°C  %.0fW",
				g.Util, g.MemUsed, g.MemTotal, pct, g.Temp, g.Power)))
		}
	}
	return b.String()
}

func (a *App) requestsPane() string {
	var b strings.Builder
	b.WriteString(titleSt.Render("REQUESTS"))
	if err := a.store.Err(); err != nil {
		b.WriteString("\n" + warnSt.Render("proxy failed: "+err.Error()))
		return b.String()
	}
	reqs := a.store.Recent(a.reqRows())
	if len(reqs) == 0 {
		b.WriteString("\n" + dimSt.Render("none yet — point your client at http://"+a.listen+" (e.g. OLLAMA_HOST="+a.listen+") to see live requests"))
		return b.String()
	}
	b.WriteString("\n" + dimSt.Render(fmt.Sprintf("%-9s %-26s %10s %6s %8s  %s", "TIME", "MODEL", "TOK/S", "OUT", "PROMPT", "TOTAL")))
	for _, r := range reqs {
		b.WriteString(fmt.Sprintf("\n%-9s %-26s %10.1f %6d %8d  %s",
			r.When.Format("15:04:05"), r.Model, r.TokSec, r.OutTk, r.PromptTk, r.Total.Round(10*time.Millisecond)))
	}
	return b.String()
}

func (a *App) byModelPane() string {
	var b strings.Builder
	b.WriteString(titleSt.Render("BY MODEL"))
	stats := a.store.ByModel()
	if len(stats) == 0 {
		b.WriteString("\n" + dimSt.Render("no traffic yet"))
		return b.String()
	}
	b.WriteString("\n" + dimSt.Render(fmt.Sprintf("%-26s %6s %10s %9s %9s %10s", "MODEL", "REQS", "AVG TOK/S", "P50", "P95", "TOKENS OUT")))
	for _, m := range stats {
		b.WriteString(fmt.Sprintf("\n%-26s %6d %10.1f %9.1f %9.1f %10d",
			m.Model, m.Count, m.AvgTok, m.P50, m.P95, m.OutTk))
	}
	return b.String()
}

func (a *App) throughputPane() string {
	rates := a.store.TokRates(60)
	title := titleSt.Render("TOK/S")
	if len(rates) == 0 {
		return title + " " + dimSt.Render("waiting for traffic")
	}
	peak := 0.0
	for _, v := range rates {
		if v > peak {
			peak = v
		}
	}
	p50, p95 := a.store.Percentiles()
	return fmt.Sprintf("%s %s  %.1f %s", title, selSt.Render(sparkline(rates, a.w-44)), rates[len(rates)-1],
		dimSt.Render(fmt.Sprintf("(peak %.0f · p50 %.0f · p95 %.0f)", peak, p50, p95)))
}

func gib(n int64) string {
	return fmt.Sprintf("%.1fG", float64(n)/(1<<30))
}
