.PHONY: build install clean test

build:
	go build -o orc ./cmd/orc/

install:
	go install ./cmd/orc/

test:
	go test ./... -count=1

clean:
	rm -f orc
