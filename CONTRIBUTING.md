# Contributing

## Pull Requests

1. Fork the repo
2. Create a feature branch (`git checkout -b feat/amazing-feature`)
3. Commit your changes (`git commit -m 'feat: add amazing feature'`)
4. Push (`git push origin feat/amazing-feature`)
5. Open a Pull Request

## Guidelines

- **Zero-dependency**: All new code must use only the Go standard library
- **Tests**: All changes must include tests; run `go test -race ./...` before pushing
- **No CGO**: The project must remain pure Go with CGO disabled
- **Style**: Run `gofmt -s -w` before committing
- **Commit messages**: Use conventional commits (`feat:`, `fix:`, `refactor:`, `docs:`, `test:`)

## Development Setup

```bash
git clone https://github.com/Sammmmmmmssssssss/chameleonnet.git
cd chameleonnet
go build ./...
go test -race ./...
```

## Code of Conduct

Be respectful, constructive, and professional.
