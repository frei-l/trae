# trae

A local trace viewer for AI apps: one Go process receives OTLP/HTTP traces
on 127.0.0.1:4318, stores them in SQLite and shows them in a MyGo window.

## Layout

- `main.go`: CLI (`trae`, `trae serve`, `trae demo`, `trae version`).
- `internal/otlp`: OTLP/HTTP receiver (protobuf and JSON, gzip).
- `internal/genai`: reads AI meaning out of span attributes: Langfuse
  `langfuse.*` first, then OpenTelemetry GenAI `gen_ai.*` (current and
  legacy). Add new conventions here, with tests.
- `internal/store`: SQLite (pure Go `modernc.org/sqlite`); `traces` rows are
  recomputed from `spans` on every insert, so batches can arrive in any
  order.
- `internal/api`: one `http.Handler` with the static UI and `/api/*` JSON. The
  app window gets it through `mygo.Protocol.Handle("mygo", …)`; `trae serve`
  serves the same handler to a browser.
- `internal/shell`: wiring for the window (`gui.go`) and the browser (`serve.go`).
- `internal/ui/web`: the frontend. Plain HTML, CSS and JS, no framework, no
  build step.

## Rules

- No cgo (MyGo and the SQLite driver are pure Go). `CGO_ENABLED=0 go build` must work.
- Frontend stays framework-free: classic `<script>` files, `el()` and
  `replaceChildren()` rendering, one state object per view, as in Magpie.
  Design tokens live at the top of `app.css`; reuse them.
- The page talks to Go only through `/api/*` (`api()` in `app.js`) and live
  updates through the `GET /api/changes?wait=1` long-poll. `window.mygo` is
  optional (`native.js`): the UI must also work in a plain browser.

## Checks

- `make test`: go vet + Go unit tests (receiver, normalizer, store, API, core).
- `make ui-test`: real OpenTelemetry JS exporters into a running trae, and the
  UI in Chrome (light/dark, wide/narrow). Set `CHROME` to the browser path and
  `ARTIFACT_DIR` to keep screenshots.
- `make serve` + `make demo` to look at the UI in a browser while working on it.
