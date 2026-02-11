.PHONY: build test run

build:
	go build ./cmd/hexlet-go-crawler

test:
	go mod tidy
	go test -v ./...

run:
	go run ./cmd/hexlet-go-crawler/main.go $(URL)
