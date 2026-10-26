.PHONY: build test lint

build:
	go build -o bin/tefrun .

test:
	go test -race ./...

lint:
	go vet ./...
	@test -z "$$(gofmt -l .)" || (gofmt -l . && echo "run gofmt -w on the files above" && exit 1)
