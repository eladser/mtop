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

	upstream := flag.String("ollama", cfg("MTOP_OLLAMA", "http://127.0.0.1:11434"), "ollama base url")
	llamacpp := flag.String("llamacpp", cfg("MTOP_LLAMACPP", "http://127.0.0.1:8080"), "llama.cpp server url (empty to skip)")
	lmstudio := flag.String("lmstudio", cfg("MTOP_LMSTUDIO", "http://127.0.0.1:1234"), "lm studio url (empty to skip)")
	vllm := flag.String("vllm", cfg("MTOP_VLLM", "http://127.0.0.1:8000"), "vllm url (empty to skip)")
	listen := flag.String("listen", cfg("MTOP_LISTEN", "127.0.0.1:4321"), "proxy listen address")
	target := flag.String("target", cfg("MTOP_TARGET", ""), "proxy upstream (defaults to the ollama url)")
	noProxy := flag.Bool("no-proxy", false, "don't run the request proxy")
	idle := flag.Duration("idle-unload", dur(cfg("MTOP_IDLE_UNLOAD", "")), "unload models with no traffic for this long (0 = off), e.g. 15m")
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
	proxyAddr := *listen
	if *noProxy {
		proxyAddr = ""
	} else {
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
	app := ui.New(scan, gpu.New(), store, proxyAddr, version, *idle)
	if _, err := tea.NewProgram(app, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// values from ~/.mtop.conf, kept out of the process environment so
// child processes like nvidia-smi don't inherit them
var conf = map[string]string{}

// ~/.mtop.conf holds MTOP_* keys, one per line, for things like
// homelab hosts you don't want to retype.
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
		if k = strings.TrimSpace(k); strings.HasPrefix(k, "MTOP_") {
			conf[k] = strings.TrimSpace(v)
		}
	}
}

// real env vars win over the conf file
func cfg(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	if v := conf[k]; v != "" {
		return v
	}
	return def
}

func dur(s string) time.Duration {
	d, _ := time.ParseDuration(s)
	return d
}
