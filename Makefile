.PHONY: build install test vet lint sec fmt gen gen-docs verify clean help sync-schema

GO ?= go
BIN := ./bin/flightline
PKG := ./...

help:
	@echo "Flightline Makefile targets:"
	@echo "  build    Build $(BIN)"
	@echo "  install  go install ."
	@echo "  test     go test -race"
	@echo "  vet      go vet"
	@echo "  lint     golangci-lint run"
	@echo "  sec      gosec security scan (standalone, all rules)"
	@echo "  fmt      gofmt + goimports"
	@echo "  gen      Reserved (codegen rejected — see .project/issues/closed/ISSUE-001)"
	@echo "  verify   vet + test + lint (what the verify hook runs at gates)"
	@echo "  clean    Remove build artifacts"

build:
	$(GO) build -o $(BIN) .

install:
	$(GO) install .

test:
	$(GO) test $(PKG) -count=1 -race

vet:
	$(GO) vet $(PKG)

lint:
	golangci-lint run

# G501/G401: MD5 is mandated by Apple's asset-upload checksum protocol, not a security choice.
# G304: this CLI opens user-specified paths (uploads, config, screenshots, state) by design.
sec:
	gosec -exclude-generated -severity low -confidence low -exclude=G501,G401,G304 ./...

fmt:
	gofmt -s -w .
	goimports -w .

# Codegen rejected — Apple's ASC OpenAPI spec hits cascading type-name and
# enum-constant collisions in every Go OpenAPI generator we evaluated. The ASC
# client at internal/asc/ is hand-rolled instead, with openapi.oas.json as
# authoritative reference (queried via jq during command authoring).
# See .project/issues/closed/ISSUE-001-oapi-codegen-collisions.md.
gen:
	@echo "flightline: codegen is intentionally not used — internal/asc/ is hand-rolled."
	@echo "flightline: spec is authoritative reference. Query via jq:"
	@echo "  jq '.paths | keys[]' openapi.oas.json | grep -i <resource>"
	@echo "  jq '.components.schemas.<Name>' openapi.oas.json"

gen-docs:
	go run ./cmd/gen-docs

verify: sync-schema vet test lint

# Keep internal/config/schema.json in sync with the canonical schema.
# `go:embed` forbids `..` traversal, so the validator's embedded copy
# lives next to its package; this target enforces byte-identity.
sync-schema:
	@cp schemas/flightline.schema.json internal/config/schema.json

clean:
	rm -rf ./bin coverage.out coverage.html
