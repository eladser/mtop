package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eladser/mtop/internal/compare"
	"github.com/eladser/mtop/internal/gpu"
	"github.com/eladser/mtop/internal/notify"
	"github.com/eladser/mtop/internal/ollama"
	"github.com/eladser/mtop/internal/proxy"
	"github.com/eladser/mtop/internal/sources"
	"github.com/eladser/mtop/internal/ui"
)

// set at build time: -ldflags "-X main.version=..."
var version = "dev"

func main() {
	loadConf()
	if len(os.Args) > 1 && os.Args[1] == "compare" {
		runCompare(os.Args[2:])
		return
	}
	runTop()
}

func runCompare(args []string) {
	fs := flag.NewFlagSet("compare", flag.ExitOnError)
	base := fs.String("ollama", cfg("MTOP_OLLAMA", "http://127.0.0.1:11434"), "ollama base url")
	openai := fs.String("openai", "", "openai-style base url instead of ollama, e.g. http://127.0.0.1:8080/v1")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, `usage: mtop compare [-ollama url | -openai url] "<prompt>" model [model ...]`)
		fs.PrintDefaults()
	}
	fs.Parse(args)
	rest := fs.Args()
	if len(rest) < 2 {
		fs.Usage()
		os.Exit(2)
	}
	if *openai != "" {
		fmt.Print(compare.Table(compare.RunOpenAI(*openai, rest[0], rest[1:])))
		return
	}
	fmt.Print(compare.Table(compare.Run(*base, rest[0], rest[1:])))
}

func runTop() {
	upstream := flag.String("ollama", cfg("MTOP_OLLAMA", "http://127.0.0.1:11434"), "ollama base url")
	llamacpp := flag.String("llamacpp", cfg("MTOP_LLAMACPP", "http://127.0.0.1:8080"), "llama.cpp server url (empty to skip)")
	lmstudio := flag.String("lmstudio", cfg("MTOP_LMSTUDIO", "http://127.0.0.1:1234"), "lm studio url (empty to skip)")
	vllm := flag.String("vllm", cfg("MTOP_VLLM", "http://127.0.0.1:8000"), "vllm url (empty to skip)")
	listen := flag.String("listen", cfg("MTOP_LISTEN", "127.0.0.1:4321"), "proxy listen address")
	target := flag.String("target", cfg("MTOP_TARGET", ""), "proxy upstream (defaults to the ollama url)")
	noProxy := flag.Bool("no-proxy", false, "don't run the request proxy")
	idle := flag.Duration("idle-unload", dur(cfg("MTOP_IDLE_UNLOAD", "")), "unload models with no traffic for this long (0 = off), e.g. 15m")
	notifyOn := flag.Bool("notify", false, "desktop notification when a gpu hits the alert line")
	history := flag.Bool("history", false, "remember recent requests across restarts (~/.mtop/history.jsonl)")
	memAlert := flag.Int("mem-alert", 93, "gpu memory percent that turns the alert line on")
	tempAlert := flag.Int("temp-alert", 87, "gpu temperature in celsius that turns the alert line on")
	inspect := flag.Bool("inspect", false, "capture prompt and completion text so the inspector (i) can show them")
	showVer := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVer {
		fmt.Println("mtop", version)
		return
	}
	// -ollama can be a comma list to watch a few boxes at once
	hosts := strings.Split(*upstream, ",")
	if *target == "" {
		*target = strings.TrimSpace(hosts[0])
	}

	store := proxy.NewStore(256)
	if *history {
		wireHistory(store)
	}

	proxyAddr := *listen
	if *noProxy {
		proxyAddr = ""
	} else {
		p, err := proxy.New(*target, store, *inspect)
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

	var notifier func(string)
	if *notifyOn {
		notifier = func(msg string) { notify.Send("mtop", msg) }
	}

	var olls []*ollama.Client
	for _, h := range hosts {
		if h = strings.TrimSpace(h); h != "" {
			olls = append(olls, ollama.New(h))
		}
	}
	scan := sources.New(olls, *llamacpp, *lmstudio, *vllm)
	app := ui.New(scan, gpu.New(), store, proxyAddr, version, *idle, notifier, *memAlert, *tempAlert, *inspect)
	if _, err := tea.NewProgram(app, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

const historyMax = 256

func historyFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".mtop", "history.jsonl")
}

func wireHistory(store *proxy.Store) {
	path := historyFile()
	if path == "" {
		return
	}
	os.MkdirAll(filepath.Dir(path), 0o700)
	old := readHistory(path)
	store.Preload(old)
	writeHistory(path, old) // trim the file back to what we kept
	store.OnAdd(func(r proxy.Request) {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return
		}
		defer f.Close()
		if b, err := json.Marshal(r); err == nil {
			f.Write(append(b, '\n'))
		}
	})
}

// readHistory loads the file (oldest first) and returns the last
// historyMax requests newest first, the order the store wants.
func readHistory(path string) []proxy.Request {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var all []proxy.Request
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		var r proxy.Request
		if json.Unmarshal(sc.Bytes(), &r) == nil {
			all = append(all, r)
		}
	}
	if len(all) > historyMax {
		all = all[len(all)-historyMax:]
	}
	for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
		all[i], all[j] = all[j], all[i]
	}
	return all
}

// writeHistory rewrites the file from a newest-first slice, oldest line
// first, so the next run reads it back in order.
func writeHistory(path string, reqs []proxy.Request) {
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()
	for i := len(reqs) - 1; i >= 0; i-- {
		if b, err := json.Marshal(reqs[i]); err == nil {
			f.Write(append(b, '\n'))
		}
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
