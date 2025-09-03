build: 
	go build -o bin/postfix_exporter .

# Useful for building in same condition as CI
build-release:
	goreleaser build

lint:
	golangci-lint run

test:
	go test ./...

# Note: we can not use gofumpt directly with golines, it's not stable
format-check:
	wsl ./...
	gofumpt -d -e .
	golines --base-formatter='gofmt' --dry-run . 
	
format:
	wsl --fix ./...
	gofumpt -w .
	golines --base-formatter='gofmt' -w .
	
format-install-tools:
	go install mvdan.cc/gofumpt@v0.8.0
	go install github.com/bombsimon/wsl/v5/cmd/wsl@v5.1.0
	go install github.com/segmentio/golines@v0.12.2

generate:
	go install go.uber.org/mock/mockgen@v0.4.0
	go generate ./...

.PHONY: build build-release lint test format-check format format-install-tools generate