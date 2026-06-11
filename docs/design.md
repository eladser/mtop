# mtop design notes

## What it is

A live terminal dashboard for the local-AI stack. You run models with Ollama, llama.cpp, LM Studio or vLLM; mtop shows what's actually happening: loaded models, VRAM, tok/s, request log, GPU state. The thing people otherwise approximate with `nvtop` + server logs + guesswork.

Tagline: "htop for your local AI."

## Why

Everyone running local models eventually asks "why is this slow", "what's eating my VRAM", or "is it even using the GPU". The existing answers are either chat clients (oterm, parllama), one-shot benchmarks, cluster-ops tooling (DCGM, the inference-cluster llmtop), or a full Grafana stack nobody sets up for their desktop. Nothing owned the single-machine view when this started, so mtop does.

## Layout

```
+----------------------------+------------------------------+
| MODELS                     | GPU                          |
| name, from, size, quant,   | util %, mem used/total,      |
| vram, ttl                  | temp, power                  |
+----------------------------+------------------------------+
| REQUESTS (live)  /  BY MODEL (toggle with c)              |
+-----------------------------------------------------------+
| TOK/S sparkline · last · peak · p50 · p95                 |
+-----------------------------------------------------------+
```

Keyboard-driven, htop conventions, dark, no mouse.

## Data sources

| Server | What | How |
|--------|------|-----|
| Ollama | loaded models, VRAM, ttl | `GET /api/ps`, `/api/tags` |
| Ollama | per-request tokens + timings | through the proxy only. Ollama has no metrics endpoint; `eval_count`/`eval_duration` ride on the response itself, so only the caller sees them |
| llama.cpp | model from `/props`; kv-cache + in-flight from `/metrics` | `/metrics` and `/slots` need `--metrics` / `--slots` on launch |
| LM Studio | loaded models | `GET /api/v0/models`, rows with `state: "loaded"` |
| vLLM | model + cache use + running count | `/metrics`, prometheus text, model name from the label |
| GPU | util, mem, temp, power | `nvidia-smi` / `rocm-smi` query output, no cgo. Apple Silicon: unified-memory from `sysctl` + `vm_stat`, plus gpu utilization from `powermetrics` when mtop runs as root |

The proxy is the only piece that needs anything from the user: one env var (`OLLAMA_HOST=127.0.0.1:4321`, or `/v1` as an OpenAI base url). Bytes pass through untouched; the tap reads each line looking for the final chunk (ollama) or the usage block (openai-style). It stops buffering at 1 MiB if it hasn't found one, so a giant response still reaches the client whole, it just doesn't get counted. Ollama's tok/s comes from its own timings. Openai-style has none, so it's tokens over wall time, stamped before the round trip so prompt processing is in there. Without the proxy the models and GPU panes still work, and the requests pane says how to fix itself.

Traffic through the proxy is plain http with no auth. Fine on loopback, a bad idea anywhere else, so binding `-listen` to a non-loopback address prints a warning.

The proxy forwards everything to ollama, including the endpoints that delete or pull models, so it checks the Host and Origin on the way in. A request has to come in with a loopback Host (or the address you bound to), and any Origin header has to be local. That stops a web page you have open from POSTing to the proxy or rebinding dns to read it. The browser shares your loopback, so binding to 127.0.0.1 isn't enough by itself. CLI clients and SDKs send neither header and call with a loopback Host, so nothing changes for them.

`/metrics` on the proxy port re-exports what the tap has seen plus the last GPU read, prometheus format.

`mtop compare "<prompt>" model...` is a separate subcommand, not part of the dashboard. It sends the prompt to each model in turn (sequential, so they don't share the GPU mid-run) and prints tok/s, tokens, and total time, fastest first. Useful for "is the bigger model actually worth the slowdown" without eyeballing the requests pane.

With `-history` the proxied requests get appended to `~/.mtop/history.jsonl` and read back on the next start, so the stats panes aren't empty after a restart. The file is trimmed to the last 256 on load. `-notify` fires a desktop notification (osascript, notify-send, or a powershell toast, whatever the OS has) the first time a GPU crosses the alert line.

## Stack

Go + bubbletea + lipgloss. It's what this genre of tool is built with, the rendering problems are solved, and it cross-compiles to single static binaries for all three platforms in one CI job.

## Unloading models

Ollama is supposed to evict idle models and sometimes doesn't. A model can sit past its expiry holding VRAM until someone runs `ollama stop`. mtop marks those rows overdue. `u` unloads the selected model with a generate call carrying `keep_alive: 0`, which is all `ollama stop` does anyway. `-idle-unload 15m` does the same automatically, off the last traffic seen through the proxy. Other servers don't expose an unload, so `u` on their rows just says so.

## Non-goals

- Not a chat client.
- Not a cloud-spend tracker.
- Not cluster ops.
- No telemetry, no accounts, no cloud. Local tool for local AI.

## Known risks

Ollama could ship a built-in `ollama top` someday. If it does, mtop still has the part ollama won't build: one view across four servers plus the GPU. The proxy needs a one-line config change and some people won't bother, which is fine, half the tool works without it. And tok/s for openai-style requests folds in prompt processing because it's wall-clock, which the FAQ says out loud instead of hiding.
