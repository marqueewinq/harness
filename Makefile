.PHONY: build test install

build:
	go build -o harness ./main.go

test:
	go test ./...

install:
	go install ./...
