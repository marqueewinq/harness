.PHONY: build test test-race lint fmt-check install

fmt-check:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

lint: fmt-check
	go vet ./...

test: lint
	go test ./...

test-race:
	go test -race ./...

build: test
	go build -o harness .

install: test
	go install ./...
	@ln -sf "$$(go env GOPATH)/bin/harness" ~/.local/bin/harness
