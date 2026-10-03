# trae tests (Node)

Run from this directory after `npm install`, or `make ui-test` from the root.
Both files build trae with Go and start `trae serve` on free ports with a
throwaway data directory (`helpers.cjs`).

- `otel-js.test.cjs`: the real `@opentelemetry/exporter-trace-otlp-proto` and
  `-http` (JSON) exporters send Langfuse-style and `gen_ai.*` spans; the test
  checks what the API returns.
- `ui.test.cjs`: Chrome via `playwright-core`. Traces list, trace detail
  (tree, keyboard, tabs, folding), filters and search, live updates and
  pause, LLM call expansion, settings (theme, retention, clear). Fails on any
  page error.

Environment: `CHROME` (browser executable, default `/usr/bin/google-chrome`
or the macOS Chrome), `ARTIFACT_DIR` (save screenshots), `GO` (go binary).
