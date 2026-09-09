# Contributing to axon

Thanks for your interest in contributing!

## Getting Started

1. Fork the repository
2. Clone your fork locally
3. Create a feature branch: `git checkout -b feat/my-change`
4. Make your changes
5. Run the gate: `make check`
6. Commit using
   [Conventional Commits](https://conventionalcommits.org)
7. Push and open a Pull Request

## Development Setup

All `make` targets run from the repo root. The Go module lives in `go/` and
the Makefile enters it for you (`go -C go ...`), so there is no directory to
change into first.

```sh
mise install    # pinned Go, Node, Python, and lint/link tools
make setup      # go mod download, plus lychee if missing
```

`make check` is the full gate — `lint test links generate-check test-parity
test-ts test-py` — and covers all three languages, not just Go.

## Code Style

- Follow existing conventions in the codebase
- Run linters before submitting: `make lint`
- Keep changes focused; one concern per PR

## Commit Messages

Use [Conventional Commits](https://conventionalcommits.org):

```
feat(scope): add new feature
fix(scope): correct a bug
docs: update readme
test: add missing tests
```

## Pull Requests

- Reference related issues in the PR description
- Keep PRs small and reviewable
- Ensure CI passes before requesting review
- Update documentation if behavior changes

## Issues

- Search existing issues before opening a new one
- Use issue templates when available
- Provide reproduction steps for bugs

## Code of Conduct

Be respectful and constructive. We are all here to build
something great together.

This project follows the
[hop-top Code of Conduct](https://github.com/hop-top/.github/blob/main/CODE_OF_CONDUCT.md).
