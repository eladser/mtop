# Changelog

## 1.5.0 (2026-10-XX)

### Added
- Partial-offload warning: an Ollama model split between GPU and system RAM gets a warn-colored row and `cpu 38%` next to its ttl. A model fully on CPU shows `cpu` in the VRAM column.
- Context-overflow warning in the requests pane (`ctx 97%`, or `ctx over, rejected`) and the inspector. Three signals: a 4xx context error from llama.cpp, vLLM, SGLang or Ollama; Ollama prompts at 90% or more of `context_length`, which it truncates silently; and `usage.prompt_tokens` (or llama.cpp timings) against the context size llama.cpp, LM Studio and Lemonade report.
- llama-swap (found through `-llamacpp`, or `-llamaswap`), Lemonade (`-lemonade`, :13305) and SGLang (`-sglang`, :30000). `u` unloads on llama-swap and Lemonade.
- AMD GPUs via amd-smi, ROCm 6 and 7 output.
- AMD and Intel GPUs on Windows through performance counters: util and memory, no temperature or power.
- `/metrics` gains `mtop_model_cpu_percent` and `mtop_ctx_overflow_total`.
- `-notify` also fires when a model loads partly on CPU and when a model's requests run over context (once a minute per model at most).

### Changed
- amd-smi is preferred over rocm-smi when both are installed.
- Bubble Tea v2 and Lip Gloss v2.
- The proxy peeks at request bodies (for the model name) up to 8 MiB; anything larger passes through unlabelled.

### Fixed
- An older Lemonade on :8000 showed up as vLLM.
- The by-model view reshuffled rows that tied.
- llama-swap on :8080 also showed up as a second llama.cpp row.

## 1.4.0 (2026-09-30)

### Added
- The proxy records `/v1/responses` requests, streamed and not.
- llama.cpp router mode: a llama-server started without `-m` lists every loaded model instead of one "(model)" row.
- LM Studio's v1 REST API (`/api/v1/models`, 0.4 and later), with a fallback to v0 for older installs.

### Fixed
- vLLM kv-cache always read 0% on vLLM 0.12+, which renamed the metric to `vllm:kv_cache_usage_perc`. Both names are read now.
- Any server answering on a probed port (a dev server on :8000, say) showed up as a phantom vllm or llama.cpp row. Non-2xx responses now count as nothing there.
- Streamed OpenAI-style requests without a `usage` block weren't recorded. They are now, using llama.cpp's own timings when present and a chunk-count estimate otherwise (marked as estimated). The proxy still passes the stream through untouched.
- nvidia-smi fields reported as `[N/A]` showed as 0; they show as `n/a` and stay out of the energy total.
- An unreadable rocm-smi response left the GPU pane blank with no error.
- Model names with unusual characters could break the unload request.

## 1.3.0 (2026-06-18)

### Added
- Multi-host: `-ollama` takes a comma list and stacks models and GPUs from each box.
- GPU util and memory sparklines.
- Request inspector (`-inspect`, then `i`): last prompt, completion, and a load/prompt/decode split.
- Session energy on the TOK/S line: watt-hours and tokens per watt-hour.
- `compare -openai <url>` for llama.cpp, LM Studio or vLLM.
- `-mem-alert` and `-temp-alert` thresholds.

## 1.2.0 (2026-06-11)

### Added
- `mtop compare "<prompt>" model1 model2`: same prompt, several ollama models, tok/s side by side.
- `-notify`: desktop notification when a GPU crosses the alert line.
- `-history`: recent requests kept in `~/.mtop/history.jsonl` across restarts.
- Real GPU utilization on Apple Silicon when run with sudo.
- GPU pane shows VRAM held by loaded models; `/metrics` includes GPU gauges.

## 1.1.3 (2026-06-10)

### Security
- The proxy only answers loopback callers and rejects cross-origin requests, so a browser tab can't drive the ollama API through it.

### Fixed
- `/metrics` per-model rows and percentiles could disagree under load.
- Footer showed the proxy address with `-no-proxy`.
- The 1 MiB stream cap wasn't applied to the final flush.

## 1.1.2 (2026-06-10)

### Fixed
- Proxy stops buffering after 1 MiB while looking for the final chunk.
- `~/.mtop.conf` values no longer leak into the environment of child processes.
- Warning when the proxy binds beyond loopback.
- golang.org/x/text updated past known CVEs.

## 1.1.1 (2026-06-10)

### Fixed
- Model rows wrapped and broke the layout with more than one server up.

## 1.1.0 (2026-06-10)

### Added
- llama.cpp, LM Studio and vLLM sources.
- OpenAI-style requests counted by the proxy.
- AMD GPUs via rocm-smi; unified memory on Apple Silicon.
- Per-model stats table (`c`), `/metrics` endpoint, alert colors, `~/.mtop.conf`.

## 1.0.0 (2026-06-09)

First release: models, GPU, requests and throughput panes, per-request tok/s through a pass-through proxy, model unload, and `-idle-unload`.
