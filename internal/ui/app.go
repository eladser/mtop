package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/eladser/mtop/internal/gpu"
	"github.com/eladser/mtop/internal/ollama"
	"github.com/eladser/mtop/internal/proxy"
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
	oll       *ollama.Client
	gpu       *gpu.Reader
	store     *proxy.Store
	listen    string
	idleAfter time.Duration

	w, h   int
	sel    int
	models []ollama.Model
	disk   int
	ollErr error
	gpus   []gpu.Stats
	gpuErr error

	// when mtop first saw each model loaded, for idle tracking
	seen    map[string]time.Time
	flash   string
	flashAt time.Time
	flashOk bool
}

func New(oll *ollama.Client, g *gpu.Reader, store *proxy.Store, listen string, idleAfter time.Duration) *App {
	return &App{oll: oll, gpu: g, store: store, listen: listen, idleAfter: idleAfter, seen: map[string]time.Time{}}
}

type tick struct{}

type data struct {
	models []ollama.Model
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
	d.models, d.ollErr = a.oll.Loaded()
	if d.ollErr == nil {
		d.disk, _ = a.oll.OnDisk()
	}
	if a.gpu.Available() {
		d.gpus, d.gpuErr = a.gpu.Read()
	}
	return d
}

func (a *App) unload(name string, auto bool) tea.Cmd {
	return func() tea.Msg {
		return unloaded{model: name, err: a.oll.Unload(name), auto: auto}
	}
}

// idle returns a model that hasn't served a proxied request (and that
// mtop has watched) for longer than idleAfter, if any.
func (a *App) idle() (string, bool) {
	if a.idleAfter <= 0 {
		return "", false
	}
	for _, m := range a.models {
		ref := a.seen[m.Name]
		if last := a.store.LastSeen(m.Name); last.After(ref) {
			ref = last
		}
		if !ref.IsZero() && time.Since(ref) > a.idleAfter {
			return m.Name, true
		}
	}
	return "", false
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
			if a.sel < len(a.models)-1 {
				a.sel++
			}
		case "u":
			if a.sel < len(a.models) {
				return a, a.unload(a.models[a.sel].Name, false)
			}
		}
	case data:
		a.models, a.disk, a.ollErr = m.models, m.disk, m.ollErr
		a.gpus, a.gpuErr = m.gpus, m.gpuErr
		if a.sel >= len(a.models) {
			a.sel = max(0, len(a.models)-1)
		}
		now := time.Now()
		alive := map[string]bool{}
		for _, mod := range a.models {
			alive[mod.Name] = true
			if _, ok := a.seen[mod.Name]; !ok {
				a.seen[mod.Name] = now
			}
		}
		for name := range a.seen {
			if !alive[name] {
				delete(a.seen, name)
			}
		}
		next := tea.Tick(pollEvery, func(time.Time) tea.Msg { return tick{} })
		if name, ok := a.idle(); ok {
			delete(a.seen, name) // don't fire again while the unload is in flight
			return a, tea.Batch(next, a.unload(name, true))
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
	bottom := paneSt.Width(a.w - 4).Render(a.requestsPane())
	spark := paneSt.Width(a.w - 4).Render(a.throughputPane())
	return lipgloss.JoinVertical(lipgloss.Left, top, bottom, spark, a.statusLine())
}

func (a *App) statusLine() string {
	help := "  ↑/↓ select · u unload · q quit · proxy on " + a.listen
	if a.flash != "" && time.Since(a.flashAt) < 12*time.Second {
		st := selSt
		if !a.flashOk {
			st = warnSt
		}
		return st.Render("  "+a.flash) + dimSt.Render(" ·"+help[2:])
	}
	return dimSt.Render(help)
}

func (a *App) modelsPane() string {
	var b strings.Builder
	b.WriteString(titleSt.Render("MODELS"))
	if a.disk > 0 {
		b.WriteString(dimSt.Render(fmt.Sprintf("  %d on disk", a.disk)))
	}
	switch {
	case a.ollErr != nil:
		b.WriteString("\n" + dimSt.Render("ollama not reachable — is it running?"))
	case len(a.models) == 0:
		b.WriteString("\n" + dimSt.Render("nothing loaded — run a model and it shows up here"))
	default:
		b.WriteString("\n" + dimSt.Render(fmt.Sprintf("  %-22s %6s %6s %6s  %s", "NAME", "SIZE", "QUANT", "VRAM", "TTL")))
		for i, m := range a.models {
			ttl := ttlFor(m)
			line := fmt.Sprintf("%-22s %6s %6s %6s  %s",
				m.Name, m.Details.ParameterSize, m.Details.QuantizationLevel, gib(m.SizeVRAM), ttl)
			if i == a.sel {
				b.WriteString("\n" + selSt.Render("▸ "+line))
			} else {
				b.WriteString("\n  " + line)
			}
		}
	}
	return b.String()
}

func ttlFor(m ollama.Model) string {
	if m.Overdue() {
		return warnSt.Render("overdue — press u")
	}
	if m.ExpiresAt.IsZero() {
		return "—"
	}
	return time.Until(m.ExpiresAt).Round(time.Second).String()
}

func (a *App) gpuPane() string {
	var b strings.Builder
	b.WriteString(titleSt.Render("GPU"))
	switch {
	case !a.gpu.Available():
		b.WriteString("\n" + dimSt.Render("no nvidia-smi found (AMD/Apple: see roadmap)"))
	case a.gpuErr != nil:
		b.WriteString("\n" + dimSt.Render("nvidia-smi error: "+a.gpuErr.Error()))
	default:
		for _, g := range a.gpus {
			pct := 0
			if g.MemTotal > 0 {
				pct = g.MemUsed * 100 / g.MemTotal
			}
			b.WriteString(fmt.Sprintf("\n%s\nutil %3d%%  mem %d/%d MiB (%d%%)  %d°C  %.0fW",
				g.Name, g.Util, g.MemUsed, g.MemTotal, pct, g.Temp, g.Power))
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
	reqs := a.store.Recent(8)
	if len(reqs) == 0 {
		b.WriteString("\n" + dimSt.Render("none yet — point your client at http://"+a.listen+" (e.g. OLLAMA_HOST="+a.listen+") to see live requests"))
		return b.String()
	}
	b.WriteString("\n" + dimSt.Render(fmt.Sprintf("%-9s %-22s %10s %6s %8s  %s", "TIME", "MODEL", "TOK/S", "OUT", "PROMPT", "TOTAL")))
	for _, r := range reqs {
		b.WriteString(fmt.Sprintf("\n%-9s %-22s %10.1f %6d %8d  %s",
			r.When.Format("15:04:05"), r.Model, r.TokSec, r.OutTk, r.PromptTk, r.Total.Round(10*time.Millisecond)))
	}
	return b.String()
}

func (a *App) throughputPane() string {
	rates := a.store.TokRates(60)
	title := titleSt.Render("TOK/S")
	if len(rates) == 0 {
		return title + " " + dimSt.Render("waiting for traffic")
	}
	max := 0.0
	for _, v := range rates {
		if v > max {
			max = v
		}
	}
	return fmt.Sprintf("%s %s  %.1f %s", title, selSt.Render(sparkline(rates, a.w-30)), rates[len(rates)-1],
		dimSt.Render(fmt.Sprintf("(peak %.0f)", max)))
}

func gib(n int64) string {
	return fmt.Sprintf("%.1fG", float64(n)/(1<<30))
}
