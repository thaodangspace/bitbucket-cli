.PHONY: build install test test-verbose docs-install docs-dev docs-build docs-preview docs-generate docs-check clean fmt vet lint

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

# Regenerate the Cobra command reference.
docs-generate:
	go run ./tools/gendocs

# Verify generated command documentation is committed and current.
docs-check:
	@set -e; \
	go run ./tools/gendocs; \
	if ! git diff --quiet -- docs/src/content/docs/reference.md; then \
		echo "generated docs are stale; run make docs-generate"; \
		git checkout -- docs/src/content/docs/reference.md; \
		exit 1; \
	fi

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
