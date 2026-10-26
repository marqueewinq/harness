.PHONY: build test lint install

lint:
	go vet ./...

test: lint
	go test ./...

build: test
	go build -o harness .

install: test
	go install ./...
	@ln -sf "$$(go env GOPATH)/bin/harness" ~/.local/bin/harness
