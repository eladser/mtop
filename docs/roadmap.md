# mtop roadmap

## 1.0 (shipped)

- Ollama: loaded models, VRAM, ttl, on-disk count
- GPU via nvidia-smi
- Pass-through proxy on :4321 for per-request tok/s, tokens, duration
- Throughput sparkline
- Model unload: select + `u`, overdue markers, `-idle-unload` for the ones ollama forgets
- Single binary, zero config, win/linux/mac

## 1.1 (shipped)

- llama.cpp, LM Studio and vLLM show up next to ollama, detected on their usual ports
- OpenAI-style endpoints (`/v1/*`) counted by the proxy, so llama.cpp and LM Studio clients get tok/s too
- AMD GPUs through rocm-smi; Apple Silicon shows unified-memory use
- Per-model stats view on `c`: requests, avg tok/s, p50/p95, tokens out
- `/metrics` on the proxy port, prometheus format
- Status-line alerts when GPU memory or temperature gets ugly
- `~/.mtop.conf` for remote hosts

## 1.2 (shipped)

- Apple GPU utilization from powermetrics when mtop runs as root, memory-only otherwise
- Per-model VRAM rolled up against the GPU total in the GPU pane
- `mtop compare`: same prompt across a few models, tok/s side by side
- Desktop notifications for the GPU alerts (`-notify`)
- GPU stats in the `/metrics` output, not just request counters
- Requests survive a restart with `-history`

## 1.3

- Watch more than one host at once: give `-ollama` a comma-list and stack the models and GPUs from each box in the panes, for people running models on a couple of machines
- GPU util and memory as sparklines over time, the same treatment the tok/s line already gets
- Let `compare` hit the OpenAI endpoints too, so it's ollama vs llama.cpp on one prompt, not just two ollama models
- Alert thresholds you can set: `-mem-alert` and `-temp-alert` instead of the hardcoded 93% and 87°C

## Later / maybe

- powermetrics without sudo (a small signed helper, or live with the requirement)
- Per-model VRAM on the non-ollama servers, if they ever expose it
- A pane for embedding/reranker traffic, if that ever feels missing in practice
