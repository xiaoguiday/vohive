# Development

## Local Validation

Run the unit test suite:

```sh
go test ./...
```

Run the same local CI entry point as GitHub Actions:

```sh
make ci
```

Useful focused targets are:

- `make fmt-check`
- `make tidy-check`
- `make vet`
- `make test`
- `make race`
- `make download`

If Go is installed outside `PATH`, pass it explicitly:

```sh
GO=/usr/local/go/bin/go make ci
```

## GitHub Actions

GitHub Actions runs `.github/workflows/ci.yml` on Ubuntu with the Go version
from `go.mod`, calling `scripts/ci.sh` for formatting, module tidiness, vet,
unit tests, and race tests.

The current test suite uses loopback networking and mock command boundaries. It
does not require a modem, root privileges, or a real TUN device in CI.

## VoHive Workspace Usage

VoHive can use this repository through its workspace:

```go
replace github.com/iniwex5/vowifi-go v1.1.2 => ../vowifi-go
```
