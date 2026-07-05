SHELL := /usr/bin/env bash
.RECIPEPREFIX := >

GO ?= $(shell command -v go 2>/dev/null || printf /usr/local/go/bin/go)
CI := ./scripts/ci.sh

.PHONY: help ci download fmt-check tidy-check vet test race

help:
> @printf 'Targets:\n'
> @printf '  make ci          run the full local CI suite\n'
> @printf '  make download    download Go module dependencies\n'
> @printf '  make fmt-check   check gofmt formatting\n'
> @printf '  make tidy-check  check go.mod/go.sum tidiness\n'
> @printf '  make vet         run go vet ./...\n'
> @printf '  make test        run go test -count=1 ./...\n'
> @printf '  make race        run go test -race -count=1 ./...\n'
> @printf '\nOverride Go with: GO=/usr/local/go/bin/go make ci\n'

ci:
> GO_BIN="$(GO)" $(CI)

download:
> GO_BIN="$(GO)" $(CI) download

fmt-check:
> GO_BIN="$(GO)" $(CI) fmt

tidy-check:
> GO_BIN="$(GO)" $(CI) tidy

vet:
> GO_BIN="$(GO)" $(CI) vet

test:
> GO_BIN="$(GO)" $(CI) test

race:
> GO_BIN="$(GO)" $(CI) race
