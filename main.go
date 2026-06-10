package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eladser/mtop/internal/gpu"
	"github.com/eladser/mtop/internal/ollama"
	"github.com/eladser/mtop/internal/proxy"
	"github.com/eladser/mtop/internal/sources"
	"github.com/eladser/mtop/internal/ui"
)

// set at build time: -ldflags "-X main.version=..."
var version = "dev"

func main() {
	loadConf()

	upstream := flag.String("ollama", envOr("MTOP_OLLAMA", "http://127.0.0.1:11434"), "ollama base url")
	llamacpp := flag.String("llamacpp", envOr("MTOP_LLAMACPP", "http://127.0.0.1:8080"), "llama.cpp server url (empty to skip)")
	lmstudio := flag.String("lmstudio", envOr("MTOP_LMSTUDIO", "http://127.0.0.1:1234"), "lm studio url (empty to skip)")
	vllm := flag.String("vllm", envOr("MTOP_VLLM", "http://127.0.0.1:8000"), "vllm url (empty to skip)")
	listen := flag.String("listen", envOr("MTOP_LISTEN", "127.0.0.1:4321"), "proxy listen address")
	target := flag.String("target", envOr("MTOP_TARGET", ""), "proxy upstream (defaults to the ollama url)")
	noProxy := flag.Bool("no-proxy", false, "don't run the request proxy")
	idle := flag.Duration("idle-unload", envDur("MTOP_IDLE_UNLOAD"), "unload models with no traffic for this long (0 = off), e.g. 15m")
	showVer := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVer {
		fmt.Println("mtop", version)
		return
	}
	if *target == "" {
		*target = *upstream
	}

	store := proxy.NewStore(256)
	if !*noProxy {
		p, err := proxy.New(*target, store)
		if err != nil {
			fmt.Fprintln(os.Stderr, "bad -target url:", err)
			os.Exit(1)
		}
		go func() {
			if err := p.Listen(*listen); err != nil {
				store.SetErr(err)
			}
		}()
	}

	scan := sources.New(ollama.New(*upstream), *llamacpp, *lmstudio, *vllm)
	app := ui.New(scan, gpu.New(), store, *listen, version, *idle)
	if _, err := tea.NewProgram(app, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// ~/.mtop.conf holds MTOP_* keys, one per line, for things like
// homelab hosts you don't want to retype. Real env vars win.
func loadConf() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	f, err := os.Open(filepath.Join(home, ".mtop.conf"))
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if strings.HasPrefix(k, "MTOP_") && os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envDur(k string) time.Duration {
	d, _ := time.ParseDuration(os.Getenv(k))
	return d
}
