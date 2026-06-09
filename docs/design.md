# mtop — design

## What it is

A live terminal dashboard for the local-AI stack. You run models with Ollama / llama.cpp / LM Studio; mtop shows you what's actually happening: loaded models, VRAM pressure, tokens/sec, context fill, request log, GPU state. The thing everyone currently approximates with `nvtop` + server logs + guesswork.

Tagline: "htop for your local AI."

## Why this, why now

- Local AI is the hottest wave on GitHub (Ollama ~162k stars, Open WebUI 100k+, r/LocalLLaMA is huge and star-generous). Riding the wave is the point, not avoiding it.
- The seat is open (verified 2026-06-09):
  - `bugkill3r/aitop` — monitors cloud-agent *spend* (Claude Code / Gemini CLI bills). Not local inference.
  - `InfraWhisperer/llmtop` — "htop for your LLM inference *cluster*". DCGM, queue depth, ops audience. Not the desktop.
  - `arinbjornk/llmtop` — system monitor with LLM-generated insights. Different product.
  - `oterm`, `parllama` — chat TUIs, not monitors.
  - `howfast`, `ollama-benchmark` — one-shot benchmarks.
  - Grafana + Prometheus + DCGM — works, but it's an infra stack, not a tool. Nobody sets this up for their desktop.
  - Nothing owns "what is my local AI doing right now" on a single machine.
- The demo gif sells itself: panes lighting up while a model streams.

## Who uses it

Anyone running local models who's asked "why is this slow" or "what's eating my VRAM" or "is it even using the GPU". That's most of r/LocalLLaMA on any given day.

## Panes (v0 target)

```
+----------------------------+------------------------------+
| MODELS                     | GPU                          |
| name, size, VRAM, ttl,     | util %, VRAM used/total,     |
| backend, quant             | temp, power, sparklines      |
+----------------------------+------------------------------+
| REQUESTS (live)                                           |
| time, model, tok/s, prompt/completion tokens,             |
| context fill %, duration, status                          |
+-----------------------------------------------------------+
| THROUGHPUT  tok/s over time (sparkline), p50/p95 latency  |
+-----------------------------------------------------------+
```

Keyboard-driven, `htop` conventions. Dark, dense, no mouse needed.

## Data sources

| Backend | What | How |
|---------|------|-----|
| Ollama | loaded models, VRAM, ttl | `GET /api/ps`, `/api/tags` |
| Ollama | per-request tokens + timing | **proxy mode only.** Stock Ollama has no metrics endpoint; `/api/ps` shows loaded models, nothing per-request. The response metadata (`eval_count`, `eval_duration`) is only visible to the caller — i.e., to mtop when it sits in the middle |
| llama.cpp server | slots, kv-cache, timings | `/metrics` (prometheus), `/slots` — both need launch flags (`--metrics`, `--slots`); README must say so up front |
| vLLM | throughput, queue | `/metrics` (prometheus) |
| LM Studio | loaded models | REST `/api/v0/models` |
| GPU | util, vram, temp, power | NVML (nvidia) first; Apple Silicon (`powermetrics`/IOKit) in v0.2 — the unified-memory Mac crowd is a big slice of the launch audience; ROCm SMI after |

Request-level visibility, decided (was an open question): **proxy mode ships in v0.** It's the only way to get tok/s and context fill for Ollama, which is the headline of the demo gif — without it the REQUESTS pane is empty on the most popular backend, and an empty flagship pane at launch is fatal. It's also the one component with zero technical risk here (same interception pattern as Seerlens, already built once). `OLLAMA_HOST=localhost:11434` becomes `localhost:4321` (mtop), one env var, documented in the first screen of the README. Poll-only mode still works with no setup — models + GPU panes live, REQUESTS pane shows an honest empty state telling you how to enable the proxy.

## Stack

Recommendation: **Go + bubbletea + lipgloss**.

- It's the genre standard (every loved TUI of this generation: lazygit, lazydocker, k9s-adjacent ecosystem) — contributors expect it, and the ecosystem has solved the hard rendering problems.
- Single static binary, trivial cross-compile (win/linux/mac in one CI job), `brew install` / `scoop install` friendly. Install friction is a star-killer; this kills the friction.
- New language on the portfolio next to the .NET flagship (Seerlens) — breadth signal.

Honest alternative: .NET 10 AOT + Spectre.Console/Terminal.Gui. Plays to strength, reinforces the ".NET AI tooling guy" brand, but the TUI ecosystem is thinner and the genre crowd skews Go/Rust. Decision can flip in week 1 if Go velocity feels wrong; nothing else in this doc changes.

## v0 scope (shippable alone)

1. Ollama only. Models pane from `/api/ps` / `/api/tags` polling.
2. NVML GPU stats (nvidia first; that's the biggest single segment).
3. **Proxy mode** for the REQUESTS pane + throughput sparkline (tok/s, prompt/completion tokens, context fill, duration). One env var to enable; honest empty state without it.
4. Single binary, zero config: `mtop` finds Ollama on localhost.

Not in v0: llama.cpp/vLLM/LM Studio, Apple/AMD GPU stats, historical persistence, alerts. Roadmap.

## v0 definition of done

- The gif shows real data end-to-end: a model loading, a request streaming through the proxy, tok/s sparkline moving, GPU pane reacting. No mockups, no staged data.
- `mtop` runs on a clean Windows + Linux + macOS machine from a downloaded release binary with zero config.
- Empty states exist for: no Ollama found, no GPU, proxy not enabled. Each says what to do next in one line.
- README: gif first, install one-liner second. A stranger can go from zero to seeing their own models in under 2 minutes.

## Non-goals

- Not a chat client (oterm exists).
- Not a cloud/agent cost tracker (aitop exists).
- Not a cluster ops tool (InfraWhisperer/llmtop exists).
- No telemetry, no accounts, no cloud. It's a local tool for local AI; that stance is part of the appeal.

## Risks, stated plainly

- Ollama could ship a built-in `ollama top`. Mitigation: multi-backend is the moat — the tool's value is one view across Ollama + llama.cpp + LM Studio + GPU, which no single vendor will build.
- Category could cool. Mitigation: it's a real recurring-use tool either way; worst case it's a strong portfolio piece with modest stars.
- Proxy mode requires users to change one env var — some won't bother, and they'll see an emptier tool. Mitigation: models + GPU panes are useful with zero setup, and the empty REQUESTS pane sells the upgrade in one line.
- First Go project + first TUI at once. Mitigation: bubbletea has mature examples for exactly this shape (lazygit lineage); fall back to .NET AOT in week 1 if velocity is wrong — the design survives the swap.
