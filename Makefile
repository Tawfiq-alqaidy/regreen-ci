GOLANGCI_LINT_VERSION := v2.15.0
GOLANGCI_LINT := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
VERSION ?= dev

.PHONY: build test vet lint check

build:
	go build -ldflags "-X main.version=$(VERSION)" -o bin/regreen ./cmd/regreen

test:
	go test -race ./...

vet:
	go vet ./...

lint:
	$(GOLANGCI_LINT) run

check: vet lint test
