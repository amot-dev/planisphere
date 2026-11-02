.PHONY: all build dev test test-backend test-frontend typecheck sync-metadata clean

# Locate Wails CLI across PATH, GOPATH/bin, and ~/go/bin
WAILS ?= $(shell command -v wails 2>/dev/null || \
                 ([ -x "$$(go env GOPATH 2>/dev/null)/bin/wails" ] && echo "$$(go env GOPATH)/bin/wails") || \
                 ([ -x "$(HOME)/go/bin/wails" ] && echo "$(HOME)/go/bin/wails") || \
                 echo "wails")

# Modern Linux distros (Ubuntu 24.04+, Debian 12+, Arch, Fedora) use WebKitGTK 4.1
UNAME_S := $(shell uname -s)
BUILD_TAGS :=
ifeq ($(UNAME_S),Linux)
  ifeq ($(shell pkg-config --exists webkit2gtk-4.1 2>/dev/null && echo yes),yes)
    BUILD_TAGS := -tags webkit2_41
  endif
endif

all: build

# Build the desktop binary into build/bin/planisphere
build: sync-metadata
	@if [ ! -d "frontend/node_modules" ]; then \
		echo "==> Installing frontend dependencies..."; \
		npm --prefix frontend install; \
	fi
	@echo "==> Building with Wails ($(WAILS))..."
	$(WAILS) build $(BUILD_TAGS)

# Start live development server with hot reloading
dev: sync-metadata
	@if [ ! -d "frontend/node_modules" ]; then npm --prefix frontend install; fi
	$(WAILS) dev $(BUILD_TAGS)

# Run full backend and frontend test suite
test: test-backend test-frontend
	@echo "==> All backend and frontend tests passed!"

test-backend:
	@echo "==> Running Go backend tests..."
	go test -count=1 ./...

test-frontend: typecheck
	@echo "==> Running frontend test suite..."
	npm --prefix frontend run test

typecheck:
	@echo "==> Running frontend type check..."
	npm --prefix frontend run typecheck

sync-metadata:
	@echo "==> Synchronizing metadata from app.config.json..."
	go run scripts/sync_metadata.go

clean:
	rm -rf build/bin/ frontend/dist/
