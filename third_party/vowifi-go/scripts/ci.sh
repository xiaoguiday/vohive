#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

find_go() {
	if [[ -n "${GO_BIN:-}" ]]; then
		printf '%s\n' "$GO_BIN"
		return
	fi
	if command -v go >/dev/null 2>&1; then
		command -v go
		return
	fi
	if [[ -x /usr/local/go/bin/go ]]; then
		printf '%s\n' /usr/local/go/bin/go
		return
	fi
	printf 'go not found; set GO_BIN=/path/to/go\n' >&2
	return 127
}

GO_BIN="$(find_go)"
GOFMT_BIN="${GOFMT_BIN:-}"
if [[ -z "$GOFMT_BIN" ]]; then
	candidate="$(dirname "$GO_BIN")/gofmt"
	if [[ -x "$candidate" ]]; then
		GOFMT_BIN="$candidate"
	elif command -v gofmt >/dev/null 2>&1; then
		GOFMT_BIN="$(command -v gofmt)"
	else
		printf 'gofmt not found; set GOFMT_BIN=/path/to/gofmt\n' >&2
		exit 127
	fi
fi

run() {
	printf '\n==> %s\n' "$*"
	"$@"
}

fmt_check() {
	mapfile -d '' files < <(find . -name '*.go' -not -path './.git/*' -print0)
	if [[ ${#files[@]} -eq 0 ]]; then
		printf '\n==> no Go files found for gofmt check\n'
		return
	fi
	printf '\n==> %s -l <go files>\n' "$GOFMT_BIN"
	unformatted="$("$GOFMT_BIN" -l "${files[@]}")"
	if [[ -n "$unformatted" ]]; then
		printf 'gofmt required for:\n%s\n' "$unformatted" >&2
		printf 'Run: %s -w <files>\n' "$GOFMT_BIN" >&2
		return 1
	fi
}

tidy_check() {
	run "$GO_BIN" mod tidy -diff
}

download() {
	run "$GO_BIN" mod download
}

vet() {
	run "$GO_BIN" vet ./...
}

test_all() {
	run "$GO_BIN" test -count=1 ./...
}

race() {
	if [[ "${SKIP_RACE:-0}" == "1" ]]; then
		printf '\n==> skipping race tests because SKIP_RACE=1\n'
		return
	fi
	read -r -a packages <<< "${CI_RACE_PACKAGES:-./...}"
	run "$GO_BIN" test -race -count=1 "${packages[@]}"
}

usage() {
	cat <<'USAGE'
Usage: scripts/ci.sh [all|download|fmt|tidy|vet|test|race ...]

Environment:
  GO_BIN             path to go binary when it is not on PATH
  GOFMT_BIN          path to gofmt binary
  SKIP_RACE=1        skip race tests
  CI_RACE_PACKAGES   package pattern(s) for race tests, default: ./...
USAGE
}

if [[ $# -eq 0 || "${1:-}" == "all" ]]; then
	tasks=(download fmt tidy vet test race)
else
	tasks=("$@")
fi

printf 'Using Go: %s\n' "$("$GO_BIN" version)"
printf 'Using gofmt: %s\n' "$GOFMT_BIN"

for task in "${tasks[@]}"; do
	case "$task" in
		download) download ;;
		fmt | fmt-check) fmt_check ;;
		tidy | tidy-check) tidy_check ;;
		vet) vet ;;
		test) test_all ;;
		race) race ;;
		-h | --help | help)
			usage
			exit 0
			;;
		*)
			printf 'unknown CI task: %s\n' "$task" >&2
			usage >&2
			exit 2
			;;
	esac
done
