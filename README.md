# trae

A local trace viewer for AI apps. trae runs on your machine, receives
OpenTelemetry traces on a local port and shows them in a desktop window:
model calls with their messages, tool calls, thinking, tokens, cost, time to
first token and errors, on a span tree with a waterfall.

Nothing leaves your computer: no account, no cloud and no Docker.

- **One process, one binary.** A Go app built on [MyGo](https://mygo.egoist.dev).
  Its window uses the system webview (WKWebView, WebView2, WebKitGTK) and it
  needs no cgo.
- **Standard input.** OTLP/HTTP on `127.0.0.1:4318/v1/traces`, in protobuf or
  JSON, with or without gzip. Any OpenTelemetry SDK can send to it.
- **Understands AI spans.** It reads Langfuse attributes (`langfuse.observation.*`,
  as written by `@langfuse/tracing`) and the OpenTelemetry GenAI conventions
  (`gen_ai.*`, both current and legacy).
- **Local storage.** SQLite, kept 7 days by default, in the app's data
  directory (`~/Library/Application Support/trae` on macOS, `~/.config/trae`
  on Linux). Development builds (`go run`, `go build`, `mygo dev`) are named
  **trae Dev** and keep their data and single-instance lock apart from the
  installed app's.
- **Plain frontend.** HTML, CSS and JavaScript with no framework and no build
  step. The design follows [Magpie](https://github.com/yetone/magpie).

## Run

Requires Go 1.27+.

```sh
go run .                 # the app window
make dev                 # the app as "trae Dev" (mygo dev), relaunched on changes
go run . serve           # no window: open http://127.0.0.1:4380 in a browser
go run . demo            # send sample traces to a running trae
make app                 # packaged app in dist/ (MyGo CLI: .app, .deb, .tar.gz, …)
```

`trae --otlp 127.0.0.1:4318 --data DIR` changes the receiver address and the
data directory; `trae serve` also takes `--ui`. If the port is taken (for
example by another collector), the window still opens and the header says
so.

On macOS, closing the window keeps trae running in the Dock and receiving
traces; click the Dock icon to open it again. On Windows and Linux closing
the window quits. Release builds come from `make app` (`mygo build`), which
takes the name and version from `mygo.json` and leaves the web inspector
off.

The app icon is `resources/icon.svg`, drawn on the macOS icon grid;
`resources/icon.png` is that file exported at 1024×1024, which `mygo build`
and `mygo dev` turn into the bundle's icon. `go run` and `go build` binaries
have no bundle, so trae sets the same PNG as its Dock and window icon at
startup.

## Send traces

### Node / TypeScript

```ts
import { NodeSDK } from "@opentelemetry/sdk-node";
import { OTLPTraceExporter } from "@opentelemetry/exporter-trace-otlp-proto";
import { BatchSpanProcessor } from "@opentelemetry/sdk-trace-base";

const sdk = new NodeSDK({
  spanProcessors: [
    new BatchSpanProcessor(
      new OTLPTraceExporter({ url: "http://127.0.0.1:4318/v1/traces" }),
      { scheduledDelayMillis: 500 },
    ),
  ],
});
sdk.start();
```

**Already using Langfuse?** Your spans already carry everything trae reads.
Add the processor above next to `LangfuseSpanProcessor` in the same
`NodeSDK`, or use it instead. Your instrumentation code doesn't change.

### Python

```py
from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter

provider = TracerProvider()
provider.add_span_processor(BatchSpanProcessor(OTLPSpanExporter(endpoint="http://127.0.0.1:4318/v1/traces")))
trace.set_tracer_provider(provider)
```

### Anything else

```sh
export OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=http://127.0.0.1:4318/v1/traces
export OTEL_EXPORTER_OTLP_TRACES_PROTOCOL=http/protobuf
```

## What it reads

| | Langfuse | OpenTelemetry GenAI |
|---|---|---|
| Kind | `langfuse.observation.type` (generation, agent, tool, chain, retriever, embedding, …) | `gen_ai.operation.name` (chat, embeddings, execute_tool, invoke_agent, …) |
| Model | `langfuse.observation.model.name` | `gen_ai.response.model`, `gen_ai.request.model`, `gen_ai.provider.name` |
| Messages | `langfuse.observation.input` / `output` (pi-ai, OpenAI and Anthropic shapes) | `gen_ai.input.messages`, `gen_ai.output.messages`, `gen_ai.system_instructions`; legacy `gen_ai.prompt.N.*` / `gen_ai.completion.N.*` and message events |
| Usage | `langfuse.observation.usage_details`, `cost_details` | `gen_ai.usage.input_tokens` / `output_tokens` (or `prompt_` / `completion_tokens`) |
| Timing | `langfuse.observation.completion_start_time` → time to first token | `gen_ai.server.time_to_first_token` |
| Grouping | `langfuse.session.id`, `langfuse.user.id`, `langfuse.trace.name`, `langfuse.trace.tags` | `gen_ai.conversation.id` |

Other spans (HTTP, database, anything) appear in the tree with their
attributes and events.

## Use

- **Traces**: every trace, newest first, live. Search covers names, prompts,
  models and IDs. Filter by service, model or errors, and pause live updates.
- **Trace**: span tree with a waterfall (time to first token is hatched).
  The selected span shows its conversation, details, attributes, events and
  raw JSON.
- **LLM Calls**: every model call across traces, like a request ledger. A row
  opens in place to show what was sent and what came back.
- **Settings**: endpoint and snippets, sample traces, theme, text size,
  retention, clear. In the app, the theme also sets the native appearance,
  and the text size is the page zoom, which View → Zoom In / Zoom Out /
  Actual Size (`⌘=` `⌘-` `⌘0`) change and keep too.

Keyboard: `/` or `⌘K` search · `⌘1` / `⌘2` views · `⌘,` settings · `↑` `↓` move ·
`←` `→` fold spans · `Esc` back.

## Develop

```sh
make serve & make demo   # UI in a browser, assets read from disk: edit and reload
make test                # go vet + Go tests
make ui-test             # real OTel JS exporters + Chrome UI tests (tests/)
```

See [AGENTS.md](AGENTS.md) for the layout and conventions.
