.PHONY: all build clean run test install

all: clean test

build:
	CGO_ENABLED=0 go build -o ./bin/trace ./cmd/t

clean:
	rm -rf ./bin

install:
	go install ./cmd/t

run:
	go run ./cmd/t

test:
	go test -v ./...
