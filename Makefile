.PHONY: build install clean test e2e e2e-docker

# Load .env if present. Variables become available to recipes below.
# Missing .env is fine — the script-phase smoke test does not need the secret.
-include .env
export

build:
	go build -o orc ./cmd/orc/

install:
	go install ./cmd/orc/

test:
	go test ./... -count=1

e2e:
	go test -tags=e2e ./e2e/ -count=1 -v

e2e-docker:
	docker build -f Dockerfile.e2e -t orc-e2e .
	docker run --rm \
		-e CLAUDE_CODE_OAUTH_TOKEN \
		orc-e2e

clean:
	rm -f orc
