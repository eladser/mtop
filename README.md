![mtop — htop for your local AI](docs/img/banner.png)

![demo](docs/img/demo.gif)

One terminal window for whatever you're running locally (Ollama, llama.cpp, LM Studio, vLLM). It shows the loaded models and how much VRAM they're sitting on, the GPU, and the requests going through with their tok/s. Hit `c` to flip the middle pane to per-model stats.

It'll also kick out models that won't leave. Pick one, press `u`, gone. Ollama is supposed to unload idle models on its own and usually does, but every so often `ollama ps` shows something that expired ten minutes ago still parked on 8 gigs. mtop flags those as overdue. `-idle-unload 15m` clears them for you.

## Install

Grab a binary from [releases](https://github.com/eladser/mtop/releases), or if you have Go:

```
go install github.com/eladser/mtop@latest
```

Run `mtop`. It looks for the usual servers on their usual ports, no config file to write.

## Seeing requests

Most local servers don't expose per-request numbers anywhere. The data only exists in the response stream as it streams. So mtop runs a little pass-through proxy on `127.0.0.1:4321` and reads the numbers off the wire. Point your client at it:

```
OLLAMA_HOST=127.0.0.1:4321              # ollama clients
base_url = "http://127.0.0.1:4321/v1"   # openai-style clients
```

Your client gets the exact same bytes back. By default the proxy forwards to ollama; `-target` aims it at llama.cpp or LM Studio instead. The models and GPU panes don't need any of this.

The same port answers `/metrics` in prometheus format if you'd rather watch it from grafana.

## Keys

| key | does |
|-----|------|
| `↑`/`↓`, `k`/`j` | move the selection |
| `u` | unload the selected model |
| `c` | swap recent requests for per-model stats |
| `q` | quit |

## Flags

```
-ollama       ollama base url             (default http://127.0.0.1:11434)
-llamacpp     llama.cpp server url        (default http://127.0.0.1:8080, empty to skip)
-lmstudio     lm studio url               (default http://127.0.0.1:1234, empty to skip)
-vllm         vllm url                    (default http://127.0.0.1:8000, empty to skip)
-listen       proxy listen address        (default 127.0.0.1:4321)
-target       proxy upstream              (defaults to the ollama url)
-idle-unload  unload models idle this long, e.g. 15m (default off)
-no-proxy     don't run the proxy
```

Every flag has an `MTOP_*` env var too, and `~/.mtop.conf` can hold them so you're not retyping a homelab box:

```
MTOP_OLLAMA=http://homelab:11434
```

## FAQ

**Does the proxy add latency?**
No. Bytes go straight through and the counters get read on the way past. Same response, same speed.

**Requests pane says "none yet".**
Your client is talking to the server directly. Send it through the proxy and they'll show up. The models and GPU panes work regardless.

**What's "overdue"?**
Ollama said it'd unload a model by a certain time and didn't. Press `u`, or set `-idle-unload` and stop thinking about it.

**llama.cpp shows up thinner than ollama.**
Start it with `--metrics` (and `--slots`) for the kv-cache numbers. Without those flags it only hands out the model name.

**AMD? Mac?**
AMD works if `rocm-smi` is installed. Apple Silicon gives you the unified-memory figure. Real GPU utilization on a Mac needs `powermetrics`, which wants root, so that part isn't in yet.

**tok/s looks different between ollama and openai-style requests.**
Ollama reports its own generation timings, so that number is real decode speed. OpenAI-style responses carry no timings, so mtop divides tokens by wall-clock time, which folds in prompt processing. Close, not identical.

**Does it phone home?**
Never. No accounts, no telemetry, it only talks to your own servers. The proxy also turns away cross-origin and non-loopback callers, so a browser tab can't reach through it to your ollama.

[MIT](LICENSE)
