package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eladser/mtop/internal/gpu"
	"github.com/eladser/mtop/internal/ollama"
	"github.com/eladser/mtop/internal/proxy"
	"github.com/eladser/mtop/internal/ui"
)

func main() {
	upstream := flag.String("ollama", envOr("MTOP_OLLAMA", "http://127.0.0.1:11434"), "ollama base url")
	listen := flag.String("listen", envOr("MTOP_LISTEN", "127.0.0.1:4321"), "proxy listen address")
	noProxy := flag.Bool("no-proxy", false, "don't run the request proxy")
	flag.Parse()

	store := proxy.NewStore(256)
	if !*noProxy {
		p, err := proxy.New(*upstream, store)
		if err != nil {
			fmt.Fprintln(os.Stderr, "bad -ollama url:", err)
			os.Exit(1)
		}
		go func() {
			if err := p.Listen(*listen); err != nil {
				store.SetErr(err)
			}
		}()
	}

	app := ui.New(ollama.New(*upstream), gpu.New(), store, *listen)
	if _, err := tea.NewProgram(app, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
