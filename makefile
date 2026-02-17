.PHONY: all build clean run test install

LDFLAGS=-ldflags "-X main.commit=`git rev-parse HEAD` -X main.ref=`git rev-parse --abbrev-ref HEAD` -X main.version=`git describe --tags --always`"

all: clean test

build:
	CGO_ENABLED=0 go build $(LDFLAGS) -o ./bin/trace ./cmd/t

clean:
	rm -rf ./bin

install:
	go install $(LDFLAGS) ./cmd/t

run:
	go run $(LDFLAGS) ./cmd/t

test:
	go test -v ./...
