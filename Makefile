.PHONY: test test-core test-macos test-site test-sitebuild test-releasetag test-icongen build-core icons

test: test-core test-macos test-site test-sitebuild test-releasetag test-icongen

test-core:
	cd core && go test ./...

test-macos:
	cd macos && swift test

test-site:
	cd scripts/sitecheck && go test ./...

test-sitebuild:
	cd scripts/sitebuild && go test ./...

test-releasetag:
	cd scripts/releasetag && go test ./...

icons:
	cd scripts/icongen && go run . ../..

test-icongen:
	cd scripts/icongen && go test ./...

build-core:
	cd core && GOOS=darwin GOARCH=arm64 go build -o ../dist/darwin-arm64/model-bridge-core ./cmd/model-bridge-core
	cd core && GOOS=windows GOARCH=amd64 go build -o ../dist/windows-amd64/model-bridge-core.exe ./cmd/model-bridge-core
