.PHONY: build test lint links check clean setup symlink release \
       promote promote-alpha promote-beta promote-rc \
       promote-release generate generate-check test-parity \
       build-ts test-ts build-py test-py

check: lint test links generate-check test-parity test-ts test-py

# Every Go target runs the toolchain against the module in go/ via
# `go -C go`, which changes directory before anything else is parsed --
# no subshell, and $(CURDIR)-relative paths in other targets keep
# working. Tools that are not the go driver (golangci-lint, goreleaser)
# read their config from the working directory instead and so need a
# real `cd go &&`. -buildvcs=false is required everywhere: bare builds
# fail with "error obtaining VCS status: exit status 128" inside a git
# worktree.
generate:
	go -C go generate ./...
# generate-check gates the TS catalog too, so regenerate it here or a
# spec edit leaves the check failing on a file `generate` never touched.
	cd ts && mise exec -- pnpm install --frozen-lockfile --ignore-scripts && mise exec -- pnpm run gen:events

# The generators run with cwd=go, so the invoke generator reaches the
# canonical root spec/ as ../spec/hosts -- same path go/generate.go
# passes, keeping `go generate` and this target in agreement.
generate-check:
	go -C go run ./internal/gen/events -check -in ../spec/events.yaml
	go -C go run ./internal/gen/hosts -check -in ../spec/hosts
	go -C go run ./internal/gen/invoke -check -spec-dir ../spec/hosts
	go -C go run ./internal/gen/spec -check
	cd ts && mise exec -- pnpm install --frozen-lockfile --ignore-scripts && mise exec -- pnpm run gen:events:check

build:
	mkdir -p bin
	go -C go build -buildvcs=false -o $(CURDIR)/bin/axon ./cmd/axon

# symlink installs ./bin/axon into the first writable user-bin
# directory on $PATH ($XDG_BIN_HOME → ~/.local/bin → ~/bin on Unix;
# %USERPROFILE%\bin → %USERPROFILE%\.local\bin → %LOCALAPPDATA%\Programs
# on Windows, where the link is a .cmd shim because os.Symlink there
# requires Admin or Developer Mode). Override the target dir with
# `make symlink SYMLINK_DIR=/path/to/bin`. Re-running with the same
# target is a no-op; pass FORCE=1 to replace a link with a different
# target.
symlink: build
	kit symlink --target ./bin/axon --name axon \
		$(if $(SYMLINK_DIR),--dir $(SYMLINK_DIR),) \
		$(if $(FORCE),--force,)

test:
	go -C go test -buildvcs=false ./...

# golangci-lint resolves .golangci.yml from its working directory, and
# that config now lives only in go/ -- running it from the root fails
# with `can't load config: unsupported version of the configuration: ""`.
lint:
	cd go && mise exec -- golangci-lint run ./...

test-parity: build-ts build-py
	mise exec -- python3 tools/parity/parity.py

build-ts:
	cd ts && mise exec -- pnpm install --frozen-lockfile --ignore-scripts && mise exec -- pnpm build

test-ts: build-ts
	cd ts && mise exec -- pnpm test

# build-py creates py/.venv (gitignored) under the mise-pinned Python 3.11
# and installs the package editable with its dev extra, so pytest and
# axon resolve without publishing anywhere. `mise exec -- python3` on its
# own always resolves to mise's bare interpreter, never an activated venv
# (mise re-resolves its own tool on every `exec`), so test-py invokes the
# venv's own python directly rather than through another `mise exec`.
build-py:
	cd py && mise exec -- python3 -m venv .venv
	cd py && ./.venv/bin/python3 -m pip install --quiet --upgrade pip
	cd py && ./.venv/bin/python3 -m pip install --quiet -e ".[dev]"

test-py: build-py
	cd py && ./.venv/bin/python3 -m pytest -q

links:
	@if command -v lychee >/dev/null 2>&1; then \
		lychee --no-progress .; \
	else \
		echo "lychee not installed; skipping link check"; \
	fi

clean:
	rm -rf bin/ dist/

setup:
	go -C go mod download
	@command -v lychee >/dev/null 2>&1 || cargo install lychee

# .goreleaser.yml moved into go/ with the module it builds (main:
# ./cmd/axon), and goreleaser reads its config from the working
# directory.
release:
	cd go && goreleaser release --clean

promote:
	@scripts/promote-release.sh

promote-alpha promote-beta promote-rc promote-release:
	@scripts/promote-release.sh $(subst promote-,,$@)
