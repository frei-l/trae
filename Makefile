VERSION ?= 0.1.0-dev
LDFLAGS = -s -w -X main.version=$(VERSION)

.PHONY: build test dev serve demo app ui-test test-all

build: ## the trae binary, no cgo
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o trae .

test: ## go vet + Go tests
	go vet ./...
	go test ./...

dev: ## the app, reading internal/ui/web from disk (reload the page after edits)
	TRAE_DEV_ASSETS=$(CURDIR)/internal/ui/web go run .

serve: ## receiver + UI at http://127.0.0.1:4380, assets from disk
	TRAE_DEV_ASSETS=$(CURDIR)/internal/ui/web go run . serve

demo: ## send sample traces to a running trae
	go run . demo

app: ## packaged app for this platform (dist/), via the MyGo CLI
	go tool mygo build

ui-test: ## exporter and browser tests (needs Node and Chrome)
	cd tests && npm install --no-audit --no-fund && npm test

test-all: test ui-test
