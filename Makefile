.PHONY: build install clean test vet e2e e2e-docker

# Version metadata embedded via -ldflags. When the root VERSION file exists it is
# the source of truth: builds report v<VERSION> with a git-derived suffix so a
# local/dev build (commits past the tag, or a dirty tree) is marked, never
# claiming to be the pristine release. A clean tree exactly at tag v<VERSION>
# reports a bare v<VERSION>. With no VERSION file, fall back to git describe
# (tag + offset + SHA). GoReleaser is unaffected — it reads {{ .Version }} from
# the tag at release time. `--match 'v*'` keeps parity with horde.
VERSION := $(shell \
	if [ -f VERSION ]; then \
		base="v$$(tr -d '[:space:]' < VERSION)"; \
		desc=$$(git describe --tags --match 'v*' --always --dirty 2>/dev/null); \
		suffix=$$(printf '%s' "$$desc" | sed -n 's/^v\?[0-9][0-9.]*\(-.*\)$$/\1/p'); \
		if [ -n "$$suffix" ]; then \
			printf '%s%s' "$$base" "$$suffix"; \
		elif [ "$$desc" = "$$base" ] || [ "v$$desc" = "$$base" ] || [ "$$desc" = "$${base#v}" ]; then \
			printf '%s' "$$base"; \
		else \
			printf '%s-dev' "$$base"; \
		fi; \
	else \
		git describe --tags --match 'v*' --always --dirty 2>/dev/null || echo dev; \
	fi)
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
