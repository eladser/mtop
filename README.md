# mtop

htop for your local AI.

![demo](docs/img/demo.gif)

mtop watches whatever you're running through Ollama: loaded models and the VRAM they hold, the GPU, every request with its tok/s, and a throughput sparkline. One terminal window instead of `ollama ps` + `nvtop` + squinting at server logs.

It also unloads models that overstay. Select one, press `u`, it's gone. Ollama is supposed to evict models on its own, but anyone who's run it for a while has seen `ollama ps` showing a model that expired ten minutes ago still sitting on 8 gigs. mtop marks those as overdue, and `-idle-unload 15m` evicts them without asking.

## Install

Grab a binary from [releases](https://github.com/eladser/mtop/releases), or:

```
go install github.com/eladser/mtop@latest
```

Then run `mtop`. It finds Ollama on localhost, no config.

## Seeing requests

Ollama has no metrics endpoint — per-request numbers only exist inside the response stream itself. So mtop runs a small pass-through proxy on `127.0.0.1:4321`. Point your client at it and requests show up live:

```
OLLAMA_HOST=127.0.0.1:4321
```

The stream reaches your client byte-for-byte; mtop reads the final chunk as it passes. Models and GPU need zero setup either way.

## Keys

| key | does |
|-----|------|
| `↑`/`↓` or `k`/`j` | select a model |
| `u` | unload it now |
| `q` | quit |

## Flags

```
-ollama       ollama base url        (default http://127.0.0.1:11434)
-listen       proxy listen address   (default 127.0.0.1:4321)
-idle-unload  unload models with no traffic for this long, e.g. 15m (default off)
-no-proxy     don't run the proxy
```

`MTOP_OLLAMA` and `MTOP_LISTEN` work too.

## Scope

Ollama today; llama.cpp and LM Studio are next ([roadmap](docs/roadmap.md)). GPU stats come from nvidia-smi, so NVIDIA only for now — AMD and Apple Silicon are on the same list. No telemetry, no accounts, nothing leaves your machine.

[MIT](LICENSE)
