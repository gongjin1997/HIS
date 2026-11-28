.PHONY: all build web lint test run-api run-worker clean

SHELL := /usr/bin/env bash
export PATH := $(HOME)/go/bin:/usr/local/go/bin:$(PATH)

all: web lint test build

web:
	cd web && npm run contrast-check && npm run lint:css && npm test && npm run build

lint:
	golangci-lint run ./...

test:
	go test -race -v -count=1 ./...

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -o bin/liveops-api ./cmd/liveops-api
	CGO_ENABLED=0 go build -trimpath -o bin/liveops-worker ./cmd/liveops-worker
	CGO_ENABLED=0 go build -trimpath -o bin/liveops-node ./cmd/liveops-node
	CGO_ENABLED=0 go build -trimpath -o bin/liveopsctl ./cmd/liveopsctl

run-api:
	./bin/liveops-api --listen 0.0.0.0:8080

run-worker:
	./bin/liveops-worker --probe-listen 127.0.0.1:18085

clean:
	rm -rf bin/ web/dist/
