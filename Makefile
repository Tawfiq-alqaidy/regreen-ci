# Keep in sync with `version:` in .github/workflows/ci.yml; change both together.
GOLANGCI_LINT_VERSION := v2.15.0
GOLANGCI_LINT := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
# Set REGREEN_VERSION to stamp a version into the binary; unset, no flag is passed.
REGREEN_VERSION ?=
VERSION_LDFLAGS = $(if $(REGREEN_VERSION),-ldflags "-X main.version=$(REGREEN_VERSION)")

.PHONY: build test vet lint check

build:
	go build $(VERSION_LDFLAGS) -o bin/regreen ./cmd/regreen

test:
	go test -race ./...

vet:
	go vet ./...

lint:
	$(GOLANGCI_LINT) run

check: vet lint test
