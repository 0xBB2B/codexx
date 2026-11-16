.PHONY: test test-core test-macos test-site build-core

test: test-core test-macos test-site

test-core:
	cd core && go test ./...

test-macos:
	cd macos && swift test

test-site:
	cd scripts/sitecheck && go test ./...

build-core:
	cd core && GOOS=darwin GOARCH=arm64 go build -o ../dist/darwin-arm64/model-bridge-core ./cmd/model-bridge-core
	cd core && GOOS=windows GOARCH=amd64 go build -o ../dist/windows-amd64/model-bridge-core.exe ./cmd/model-bridge-core
