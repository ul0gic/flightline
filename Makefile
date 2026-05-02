.PHONY: build install test vet lint fmt gen verify clean help

GO ?= go
BIN := ./bin/skipper
PKG := ./...

help:
	@echo "Skipper Makefile targets:"
	@echo "  build    Build $(BIN)"
	@echo "  install  go install ./cmd/skipper"
	@echo "  test     go test -race"
	@echo "  vet      go vet"
	@echo "  lint     golangci-lint run"
	@echo "  fmt      gofmt + goimports"
	@echo "  gen      Regenerate internal/api/api.gen.go from openapi.oas.json"
	@echo "  verify   vet + test + lint (what the verify hook runs at gates)"
	@echo "  clean    Remove build artifacts"

build:
	$(GO) build -o $(BIN) ./cmd/skipper

install:
	$(GO) install ./cmd/skipper

test:
	$(GO) test $(PKG) -count=1 -race

vet:
	$(GO) vet $(PKG)

lint:
	golangci-lint run

fmt:
	gofmt -s -w .
	goimports -w .

# Codegen status: PENDING Phase 1.0 (see .project/build-plan.md).
# Apple's ASC OpenAPI spec has many type-name collisions (BuildBundleType,
# CertificateType, ActorType vs constant ActorType, PlatformSchema redeclared,
# etc.) that oapi-codegen cannot auto-resolve. Phase 1.0 evaluates:
#   1. Aggressive jq patching of x-go-name across the spec
#   2. ogen (alternative generator with different naming)
#   3. Hand-rolled thin client over net/http (Skipper uses ~30-50 endpoints,
#      writing them by hand may be cheaper than fixing codegen)
# Until Phase 1.0 lands, this target is a no-op so the scaffold builds clean.
gen:
	@echo "skipper: codegen pending Phase 1.0 — see .project/build-plan.md"
	@echo "skipper: spec has $$(jq '.paths | keys | length' openapi.oas.json) paths and $$(jq '.components.schemas | keys | length' openapi.oas.json) schemas."

verify: vet test lint

clean:
	rm -rf ./bin coverage.out coverage.html
