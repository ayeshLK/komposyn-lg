.PHONY: fmt lint test build installer-trust release-provenance release-executable-copy check release licenses clean

VERSION ?= v0.1.0
OUTPUT_DIR ?= dist

fmt:
	go fmt ./...

lint:
	go vet ./...

test:
	go test ./...

build:
	go build -o bin/komposyn-lg ./plugins/cli/token-logger

installer-trust:
	bash scripts/test-installer-trust.sh

release-provenance:
	bash scripts/test-release-provenance.sh
	bash scripts/test-release-version-flow.sh

release-executable-copy:
	bash scripts/test-copy-release-executable.sh

check: fmt lint test build installer-trust release-provenance release-executable-copy

release:
	bash scripts/build-release.sh $(VERSION) $(OUTPUT_DIR)

licenses:
	bash scripts/generate-third-party-licenses.sh $(OUTPUT_DIR)

clean:
	rm -rf bin
