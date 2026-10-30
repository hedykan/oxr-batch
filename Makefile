GOCACHE ?= $(CURDIR)/.cache/go-build

.PHONY: test build run

test:
	GOCACHE=$(GOCACHE) go test ./...

build:
	GOCACHE=$(GOCACHE) go build -buildvcs=false -o oxr-batch ./cmd/oxr-batch

run:
	GOCACHE=$(GOCACHE) go run -buildvcs=false ./cmd/oxr-batch serve
