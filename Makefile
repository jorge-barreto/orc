.PHONY: build install clean test vet e2e e2e-docker

# Version metadata embedded via -ldflags. `version` falls back to git describe
# (tag + offset + SHA) so dev builds are self-identifying; release builds get
# the clean tag from GoReleaser. `--match 'v*'` keeps parity with horde.
VERSION    := $(shell git describe --tags --match 'v*' --always --dirty 2>/dev/null || echo dev)
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS    := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)

# Load .env if present. Variables become available to recipes below.
# Missing .env is fine — the script-phase smoke test does not need the secret.
-include .env
export

build:
	go build -ldflags '$(LDFLAGS)' -o orc ./cmd/orc/

install:
	go install -ldflags '$(LDFLAGS)' ./cmd/orc/

vet:
	go vet ./...

test:
	go test ./... -count=1

e2e:
	go test -tags=e2e ./e2e/ -count=1 -v

e2e-docker:
	docker build -f Dockerfile.e2e -t orc-e2e .
	@test -f .env || (echo "error: .env not found — copy .env.example to .env and fill in CLAUDE_CODE_OAUTH_TOKEN" && exit 1)
	docker run --rm --env-file .env orc-e2e

clean:
	rm -f orc
