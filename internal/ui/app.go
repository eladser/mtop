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
	dim     = lipgloss.Color("240")
	titleSt = lipgloss.NewStyle().Foreground(accent).Bold(true)
	dimSt   = lipgloss.NewStyle().Foreground(dim)
	paneSt  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(dim).Padding(0, 1)
)

type App struct {
	oll    *ollama.Client
	gpu    *gpu.Reader
	store  *proxy.Store
	listen string

	w, h   int
	models []ollama.Model
	disk   int
	ollErr error
	gpus   []gpu.Stats
	gpuErr error
}

func New(oll *ollama.Client, g *gpu.Reader, store *proxy.Store, listen string) *App {
	return &App{oll: oll, gpu: g, store: store, listen: listen}
}

type tick struct{}

type data struct {
	models []ollama.Model
	disk   int
	ollErr error
	gpus   []gpu.Stats
	gpuErr error
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

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.w, a.h = m.Width, m.Height
	case tea.KeyMsg:
		switch m.String() {
		case "q", "ctrl+c":
			return a, tea.Quit
		}
	case data:
		a.models, a.disk, a.ollErr = m.models, m.disk, m.ollErr
		a.gpus, a.gpuErr = m.gpus, m.gpuErr
		return a, tea.Tick(pollEvery, func(time.Time) tea.Msg { return tick{} })
	case tick:
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
	help := dimSt.Render("  q quit · proxy on " + a.listen)
	return lipgloss.JoinVertical(lipgloss.Left, top, bottom, spark, help)
}

func (a *App) modelsPane() string {
	var b strings.Builder
	b.WriteString(titleSt.Render("MODELS"))
	switch {
	case a.ollErr != nil:
		b.WriteString("\n" + dimSt.Render("ollama not reachable — is it running?"))
	case len(a.models) == 0:
		b.WriteString("\n" + dimSt.Render(fmt.Sprintf("nothing loaded (%d on disk) — run a model and it shows up here", a.disk)))
	default:
		for _, m := range a.models {
			ttl := time.Until(m.ExpiresAt).Round(time.Second)
			b.WriteString(fmt.Sprintf("\n%-24s %6s %5s  vram %s  ttl %s",
				m.Name, m.Details.ParameterSize, m.Details.QuantizationLevel, gib(m.SizeVRAM), ttl))
		}
	}
	return b.String()
}

func (a *App) gpuPane() string {
	var b strings.Builder
	b.WriteString(titleSt.Render("GPU"))
	switch {
	case !a.gpu.Available():
		b.WriteString("\n" + dimSt.Render("no nvidia-smi found (AMD/Apple support: see roadmap)"))
	case a.gpuErr != nil:
		b.WriteString("\n" + dimSt.Render("nvidia-smi error: "+a.gpuErr.Error()))
	default:
		for _, g := range a.gpus {
			b.WriteString(fmt.Sprintf("\n%s\nutil %3d%%  mem %d/%d MiB  %d°C  %.0fW",
				g.Name, g.Util, g.MemUsed, g.MemTotal, g.Temp, g.Power))
		}
	}
	return b.String()
}

func (a *App) requestsPane() string {
	var b strings.Builder
	b.WriteString(titleSt.Render("REQUESTS"))
	if err := a.store.Err(); err != nil {
		b.WriteString("\n" + dimSt.Render("proxy failed: "+err.Error()))
		return b.String()
	}
	reqs := a.store.Recent(8)
	if len(reqs) == 0 {
		b.WriteString("\n" + dimSt.Render("none yet — point your client at http://"+a.listen+" (e.g. OLLAMA_HOST="+a.listen+") to see live requests"))
		return b.String()
	}
	for _, r := range reqs {
		b.WriteString(fmt.Sprintf("\n%s  %-22s %6.1f tok/s  %4d out  %4d prompt  %s",
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
	return fmt.Sprintf("%s %s  %.1f", title, sparkline(rates, a.w-20), rates[len(rates)-1])
}

func gib(n int64) string {
	return fmt.Sprintf("%.1fG", float64(n)/(1<<30))
}
