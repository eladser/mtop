# mtop — roadmap

## 1.0 — shipped

- Ollama: loaded models, VRAM, ttl, on-disk count
- GPU via nvidia-smi
- Pass-through proxy on :4321 for per-request tok/s, tokens, duration
- Throughput sparkline
- Model unload: select + `u`, overdue markers, `-idle-unload` for the ones ollama forgets
- Single binary, zero config, win/linux/mac

## 1.1 — shipped

- llama.cpp, LM Studio and vLLM show up next to ollama, detected on their usual ports
- OpenAI-style endpoints (`/v1/*`) counted by the proxy, so llama.cpp and LM Studio clients get tok/s too
- AMD GPUs through rocm-smi; Apple Silicon shows unified-memory use
- Per-model stats view on `c`: requests, avg tok/s, p50/p95, tokens out
- `/metrics` on the proxy port, prometheus format
- Status-line alerts when GPU memory or temperature gets ugly
- `~/.mtop.conf` for remote hosts

## 1.2

- Real Apple GPU utilization — powermetrics wants root, so this needs a setuid helper or a polite "run me with sudo once" story
- Per-model VRAM attribution where servers expose it
- Model comparison: same prompt against two servers, side by side
- Desktop notifications for the alerts that currently only color the status line

## Later / maybe

- Prometheus scrape of GPU stats too (only request counters today)
- Historical persistence across restarts
- A fourth pane for embedding/reranker traffic if that ever feels missing in practice
