.PHONY: fmt test vet build server

fmt:
	gofmt -w cmd

test:
	go test ./...

vet:
	go vet ./...

build:
	mkdir -p bin
	go build -trimpath -o bin/euryphos-server ./cmd/euryphos-server

server:
	go run ./cmd/euryphos-server
