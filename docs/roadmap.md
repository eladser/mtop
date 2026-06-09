# mtop — roadmap

## v0 — see your local AI (target: 3-4 weeks of evenings; first Go project, priced in)

- Ollama: models pane (`/api/ps`, `/api/tags`)
- GPU pane via NVML (nvidia)
- Proxy mode: per-request tok/s, tokens, context fill, duration (Seerlens-style interception — this feeds the REQUESTS pane and the throughput sparkline; without it Ollama exposes no per-request data at all)
- Single binary, zero config, win/linux/mac builds in CI
- Good README with the gif. The gif IS the launch asset — record it early, iterate on it like a feature.
- Done = the definition-of-done checklist in design.md, not "it mostly works".

## v0.2 — the rest of the stack

- Apple Silicon GPU/memory stats (powermetrics/IOKit) — the Mac unified-memory crowd is a big slice of r/LocalLLaMA; don't launch wide without them
- llama.cpp server (`/metrics`, `/slots` — document the required launch flags) and vLLM (`/metrics`)
- LM Studio (`/api/v0`)
- AMD (ROCm SMI)

## v0.3 — depth

- Short-term history (last hour sparklines), p50/p95
- Config file for remote hosts (homelab boxes)
- OpenAI-compatible proxy endpoint so non-Ollama clients can route through mtop too

## v1 — launch

- Polish pass: empty states, error states, tiny-terminal handling, themes
- `brew` + `scoop` + `winget` formulas, GitHub release binaries
- Launch: r/LocalLLaMA post (this is the main one — the audience lives there), Show HN, X/dev-Twitter thread with the gif
- Launch post angle: "I got tired of juggling nvtop and server logs to know what my GPU was doing, so I built htop for local AI"

## Later / maybe

- Alerts (VRAM near limit, thermal throttle) — desktop notification
- Prometheus export (become the thing Grafana users scrape, instead of competing with them)
- Model-comparison view (same prompt, two backends, side-by-side tok/s)

## Launch notes

- Multiple shots, not one: r/LocalLLaMA, HN, X, maybe a short YouTube demo. Prior research says HN front page is a lottery, not a plan — don't bank on one post.
- Post when there's something running in the gif, never a mockup. That community smells vaporware instantly.
- README structure: gif first, one-line install second, everything else third.
