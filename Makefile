MODULE := github.com/kinstall/kinstall
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo v0.1.0-dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X $(MODULE)/internal/version.Version=$(VERSION) \
	-X $(MODULE)/internal/version.Commit=$(COMMIT) \
	-X $(MODULE)/internal/version.Date=$(DATE)

.PHONY: all spec-check generate lint test test-contract build build-cross web-build clean

all: spec-check lint test build

spec-check:
	python3 spec/gen_os_docs.py --check

generate: spec-check
	@if command -v oapi-codegen >/dev/null 2>&1; then \
		oapi-codegen -config api/oapi-codegen.yaml api/openapi.yaml; \
		oapi-codegen -config api/oapi-client.yaml api/openapi.yaml; \
	else \
		go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.9.0 -config api/oapi-codegen.yaml api/openapi.yaml; \
		go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.9.0 -config api/oapi-client.yaml api/openapi.yaml; \
	fi

lint: spec-check
	go vet ./...
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	elif [ -x "$$(go env GOPATH)/bin/golangci-lint" ]; then \
		"$$(go env GOPATH)/bin/golangci-lint" run ./...; \
	else \
		echo "golangci-lint not found; run: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest"; \
		exit 1; \
	fi

test:
	CGO_ENABLED=0 go test -count=1 ./...
	go test -race -count=1 ./pkg/... ./internal/... ./plugins/...

test-contract:
	go test -tags=contract -v ./test/contract/...

web-build:
	cd web && npm ci --ignore-scripts 2>/dev/null || npm install && npm run build

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/kinstall ./cmd/kinstall

build-noui:
	mkdir -p bin
	CGO_ENABLED=0 go build -tags=noui -trimpath -ldflags "$(LDFLAGS)" -o bin/kinstall-noui ./cmd/kinstall

build-cross:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/kinstall-linux-amd64 ./cmd/kinstall
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/kinstall-linux-arm64 ./cmd/kinstall

clean:
	rm -rf bin dist
