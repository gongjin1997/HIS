MODULE := github.com/kinstall/kinstall
LDFLAGS := -s -w

.PHONY: all lint test web-build build build-cross clean

all: web-build lint test build

lint:
	go vet ./...
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	elif [ -x "$$(go env GOPATH)/bin/golangci-lint" ]; then \
		"$$(go env GOPATH)/bin/golangci-lint" run ./...; \
	else \
		echo "golangci-lint not found"; exit 1; \
	fi

test:
	go test -race -count=1 ./...

web-build:
	cd web && npm run lint:css && npm run contrast-check && npm test && npm run build

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/kinstall ./cmd/kinstall

build-cross:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/kinstall-linux-amd64 ./cmd/kinstall
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/kinstall-linux-arm64 ./cmd/kinstall

clean:
	rm -rf bin dist
