BINARY := rxctl
PKG    := ./cmd/rxctl
LDFLAGS := -s -w -X main.version=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev) \
           -X main.commit=$(shell git rev-parse --short HEAD 2>/dev/null || echo none) \
           -X main.build=$(shell date -u +%Y-%m-%d)

.PHONY: all build test vet fmt clean cross release

all: fmt vet test build

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) $(PKG)

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

cross:
	CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-amd64 $(PKG)
	CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-arm64 $(PKG)
	CGO_ENABLED=0 GOOS=linux   GOARCH=arm   GOARM=7 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-armv7 $(PKG)
	CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-darwin-arm64 $(PKG)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-windows-amd64.exe $(PKG)

# Linux 发布产物 + 校验和（上传 GitHub Release 用）
release:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-amd64 $(PKG)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-arm64 $(PKG)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm   GOARM=7 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-armv7 $(PKG)
	cd dist && (sha256sum $(BINARY)-linux-* 2>/dev/null || shasum -a 256 $(BINARY)-linux-*) > SHA256SUMS

clean:
	rm -rf bin dist
