# Repository Guidelines

## Project Structure & Module Organization

This repository is a Go TLS shunt proxy. The entry point is `main.go`; reusable configuration logic is in `config/`, connection and protocol handlers are in `handler/`, TLS sniffing code is in `sniffer/`, and DNS/HTTP2 helpers live under `handler/dns/` and `handler/http2/`. The Web UI is in `webui/`, with Go server code plus static assets and templates under `webui/static/` and `webui/templates/`. Deployment assets are in `dist/`, `deploy/`, and `tls-proxy-config/`. Example and local configuration files include `config.simple.yaml`, `config.wildcard.example.yaml`, and `template.yaml`.

## Build, Test, and Development Commands

- `go test ./...`: run all Go tests.
- `go build -o tls-shunt-proxy main.go`: build the local executable.
- `go run . -config config.simple.yaml`: run locally with the simple sample config.
- `GOOS=linux GOARCH=amd64 go build -o tls-shunt-proxy-linux-amd64 main.go`: build a Linux AMD64 binary for deployment.
- `bash tls-proxy-config/scripts/deploy.sh`: deploy to a systemd-based Linux host; review the script and target environment first.

## Coding Style & Naming Conventions

Use standard Go formatting: run `gofmt` on changed Go files before committing. Package names are short lowercase identifiers, and exported symbols should use descriptive PascalCase names. Keep YAML examples readable with two-space indentation. Preserve repository file encoding as UTF-8 without BOM and CRLF line endings.

## Testing Guidelines

Tests use Go's standard `testing` package and should be named `TestXxx` in `*_test.go` files. Add focused table tests or subtests when changing config generation, handlers, reload behavior, or protocol routing. Run `go test ./...` before opening a pull request; include manual verification notes for networking, certificate, or Web UI behavior that cannot be covered by unit tests.

## Commit & Pull Request Guidelines

Recent history uses short subjects such as `fix: update all URLs to Gitee (feixion) repo` and `feature:reload config`. Prefer concise, imperative subjects with a `fix:` or `feature:` prefix when applicable. Pull requests should include a clear summary, test results, configuration impacts, and linked issues. Add screenshots or request/response examples for Web UI and API changes.

## Security & Configuration Tips

Do not commit real credentials, Cloudflare tokens, private keys, certificates, or production `config.yaml` values. Keep local secrets in environment variables such as `WEBUI_USER` and `WEBUI_PASS`, or in untracked deployment configuration.
