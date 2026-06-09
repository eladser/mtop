# mtop — roadmap

## 1.0 — shipped

- Ollama: loaded models, VRAM, ttl, on-disk count
- GPU via nvidia-smi
- Pass-through proxy on :4321 for per-request tok/s, tokens, context, duration
- Throughput sparkline with peak
- Model unload: select + `u`, overdue markers, `-idle-unload` for the ones ollama forgets
- Single binary, zero config, win/linux/mac

## 1.1 — the rest of the stack

- Apple Silicon GPU/memory stats — the Mac unified-memory crowd is a big slice of r/LocalLLaMA; don't push the launch wide without them
- llama.cpp server (`/metrics`, `/slots` — both need launch flags, document that) and vLLM (`/metrics`)
- LM Studio (`/api/v0`)
- AMD (ROCm SMI)

## 1.2 — depth

- Short-term history (last hour sparklines), p50/p95
- Config file for remote hosts (homelab boxes)
- OpenAI-compatible proxy endpoint so non-Ollama clients can route through mtop too

## Later / maybe

- Alerts (VRAM near limit, thermal throttle) — desktop notification
- Prometheus export: become the thing Grafana users scrape instead of competing with them
- Model-comparison view (same prompt, two backends, side-by-side tok/s)

## Launch notes

- Multiple shots, not one: r/LocalLLaMA, Show HN, X with the gif. HN front page is a lottery, not a plan.
- Post when there's something running in the gif, never a mockup. That community smells vaporware instantly.
- README structure: gif first, one-line install second, everything else third.
- Launch post angle: "I got tired of juggling nvtop and server logs to know what my GPU was doing, so I built htop for local AI."
