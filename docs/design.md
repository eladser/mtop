# mtop — design notes

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
| GPU | util, mem, temp, power | `nvidia-smi` / `rocm-smi` query output, no cgo. Apple Silicon: unified-memory numbers from `sysctl` + `vm_stat`; real GPU util needs root for powermetrics, still open |

The proxy is the only piece that needs anything from the user: one env var (`OLLAMA_HOST=127.0.0.1:4321`, or `/v1` as an OpenAI base url). Bytes pass through untouched; the tap reads each line looking for the final chunk (ollama) or the usage block (openai-style). It gives up after buffering 1 MiB without finding one — a response that big still reaches the client whole, it just isn't counted. Ollama's tok/s comes from its own timings; openai-style has none, so it's tokens over wall time, stamped before the round trip so prompt processing is included. Without the proxy the models and GPU panes still work, and the requests pane says how to fix itself.

Traffic through the proxy is plain http with no auth, which is fine on loopback and a bad idea anywhere else — binding `-listen` to a non-loopback address prints a warning saying so.

`/metrics` on the proxy port re-exports what the tap has seen, prometheus format.

## Stack

Go + bubbletea + lipgloss. It's what this genre of tool is built with, the rendering problems are solved, and it cross-compiles to single static binaries for all three platforms in one CI job.

## Unloading models

Ollama is supposed to evict idle models and sometimes doesn't — a model can sit past its expiry holding VRAM until someone runs `ollama stop`. mtop marks those rows overdue, `u` unloads the selected model (a generate call with `keep_alive: 0`, which is all `ollama stop` does anyway), and `-idle-unload 15m` handles it automatically using last-traffic-seen through the proxy. Other servers don't expose an unload, so `u` on their rows just says so.

## Non-goals

- Not a chat client.
- Not a cloud-spend tracker.
- Not cluster ops.
- No telemetry, no accounts, no cloud. Local tool for local AI.

## Known risks

Ollama could ship a built-in `ollama top` someday; the answer is that mtop's value is one view across four servers plus the GPU, which no single vendor will build. The proxy needs a one-line config change and some people won't bother — that's fine, half the tool works without it. And tok/s for openai-style requests includes prompt processing (wall clock), which is documented rather than hidden.
