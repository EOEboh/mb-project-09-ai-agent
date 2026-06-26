.PHONY: run build tidy clean setup help

## run: start the development server
run:
	go run main.go

## build: compile to ./bin/app
build:
	@mkdir -p bin
	go build -o bin/app .

## tidy: download dependencies
tidy:
	go mod tidy

## clean: remove build artifacts
clean:
	rm -rf bin/

## setup: download Go dependencies and confirm Ollama model
setup:
	go mod tidy
	ollama pull llama3.2:3b

## help: list all available commands
help:
	@grep -E '^##' Makefile | sed 's/## //'