BINARY=gryph

.PHONY: build
build:
	go build -o bin/$(BINARY) ./

.PHONY: fmt
fmt:
	go fmt ./...
