.PHONY: build install test test-verbose docs-install docs-dev docs-build docs-preview clean fmt vet lint

BINARY := bitbucket-cli
MODULE := github.com/thaodangspace/bitbucket-cli

# Build the binary locally.
build:
	go build -o $(BINARY) .

# Install the binary to $GOPATH/bin (or $GOBIN).
install:
	go install .

# Run all tests.
test:
	go test ./...

# Run all tests with verbose output.
test-verbose:
	go test ./... -v

# Install documentation site dependencies.
docs-install:
	npm --prefix docs ci

# Start the documentation site development server.
docs-dev:
	npm --prefix docs run dev

# Build the static documentation site.
docs-build: docs-install
	npm --prefix docs run build

# Preview the production documentation build.
docs-preview:
	npm --prefix docs run preview

# Remove the built binary.
clean:
	rm -f $(BINARY)

# Format Go source files.
fmt:
	go fmt ./...

# Run go vet.
vet:
	go vet ./...

# Run fmt, vet, and test — standard pre-commit check.
lint: fmt vet test
