APP ?= leadfox
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo development)
LDFLAGS ?= -X main.buildVersion=$(VERSION)

.PHONY: build test vet dist release-candidate clean

build:
	go build -ldflags "$(LDFLAGS)" -o "$(APP)" .

test:
	go test ./...

vet:
	go vet ./...

dist:
	mkdir -p dist
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/$(APP)-darwin-arm64 .
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(APP)-darwin-amd64 .
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/$(APP)-linux-arm64 .
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(APP)-linux-amd64 .

release-candidate: dist
	VERSION="$(VERSION)" ./scripts/release-candidate.sh

clean:
	rm -rf dist "$(APP)"
