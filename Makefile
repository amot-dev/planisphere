.PHONY: all build dev run test test-backend test-frontend typecheck sync-metadata clean install-frontend \
        package-tarball package-appimage package-windows package-deb package-rpm package-arch

VERSION := $(shell grep -o '"version": *"[^"]*"' app.config.json 2>/dev/null | cut -d'"' -f4)
ifeq ($(VERSION),)
  $(error Version not found in app.config.json)
endif

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

ifeq ($(OS),Windows_NT)
  TARGET_BIN := build/bin/planisphere.exe
  RUN_CMD := $(TARGET_BIN)
else ifeq ($(UNAME_S),Darwin)
  TARGET_BIN := build/bin/Planisphere.app
  RUN_CMD := open $(TARGET_BIN)
else
  TARGET_BIN := build/bin/planisphere
  RUN_CMD := ./$(TARGET_BIN)
endif

# Source file dependencies for incremental compilation
GO_SOURCES := $(shell find internal -name "*.go" 2>/dev/null) main.go go.mod go.sum
FRONTEND_SOURCES := $(shell find frontend/src -type f 2>/dev/null) frontend/index.html frontend/vite.config.ts

# Synchronize metadata only when app.config.json or the generator script changes
internal/version/version.go: app.config.json scripts/sync_metadata.go
	@echo "==> Synchronizing metadata from app.config.json..."
	go run scripts/sync_metadata.go
	@touch $@

sync-metadata:
	@echo "==> Synchronizing metadata from app.config.json..."
	go run scripts/sync_metadata.go
	@touch internal/version/version.go

# Install frontend dependencies only when package manifests change or node_modules is missing
NODE_MODULES_STAMP := frontend/node_modules/.package-lock.json

$(NODE_MODULES_STAMP): frontend/package.json frontend/package-lock.json
	@echo "==> Installing frontend dependencies..."
	@if [ -f "frontend/package-lock.json" ]; then \
		(cd frontend && npm ci); \
	else \
		(cd frontend && npm install); \
	fi
	@touch $@

install-frontend: $(NODE_MODULES_STAMP)

# Compile desktop binary only when sources, metadata, dependencies, or config change
$(TARGET_BIN): $(GO_SOURCES) $(FRONTEND_SOURCES) internal/version/version.go $(NODE_MODULES_STAMP) wails.json
	@echo "==> Building with Wails ($(WAILS))..."
	$(WAILS) build $(BUILD_TAGS)

all: build

# Build the desktop binary
build: $(TARGET_BIN)

# Start live development server with hot reloading
dev: internal/version/version.go $(NODE_MODULES_STAMP)
	@echo "==> Starting Wails live development server..."
	$(WAILS) dev $(BUILD_TAGS)

# Run the desktop application (smart incremental execution)
run: $(TARGET_BIN)
	@echo "==> Running Planisphere..."
	$(RUN_CMD)

# Run full backend and frontend test suite
test: test-backend test-frontend
	@echo "==> All backend and frontend tests passed!"

test-backend:
	@echo "==> Running Go backend tests..."
	@mkdir -p frontend/dist && touch frontend/dist/index.html
	go test -count=1 ./...

test-frontend: typecheck
	@echo "==> Running frontend test suite..."
	(cd frontend && npm test)

typecheck: install-frontend
	@echo "==> Running frontend type check..."
	(cd frontend && npm run typecheck)

# Package Linux tarball distribution
package-tarball: build
	@echo "==> Packaging Linux tarball distribution (v$(VERSION))..."
	@mkdir -p dist/tarball
	@cp build/bin/planisphere dist/tarball/
	@cp build/linux/planisphere.desktop dist/tarball/
	@cp build/appicon.png dist/tarball/planisphere.png
	@cp LICENSE README.md dist/tarball/
	@tar -czvf "dist/planisphere-v$(VERSION)-linux-amd64.tar.gz" -C dist/tarball .
	@rm -rf dist/tarball
	@echo "==> Linux tarball created: dist/planisphere-v$(VERSION)-linux-amd64.tar.gz"

# Package AppImage distribution
package-appimage: build
	@echo "==> Packaging AppImage distribution (v$(VERSION))..."
	@mkdir -p dist/AppDir/usr/bin dist/AppDir/usr/share/applications dist/AppDir/usr/share/icons/hicolor/512x512/apps
	@cp -f build/bin/planisphere dist/AppDir/usr/bin/
	@cp -f build/linux/planisphere.desktop dist/AppDir/
	@cp -f build/linux/planisphere.desktop dist/AppDir/usr/share/applications/
	@cp -f build/appicon.png dist/AppDir/planisphere.png
	@cp -f build/appicon.png dist/AppDir/usr/share/icons/hicolor/512x512/apps/
	@printf '#!/bin/sh\nHERE=$$(dirname "$$(readlink -f "$$0")")\nexport PATH="$$HERE/usr/bin:$$PATH"\nexec "$$HERE/usr/bin/planisphere" "$$@"\n' > dist/AppDir/AppRun
	@chmod +x dist/AppDir/AppRun
	@bash packaging/appimage/build.sh dist/AppDir "dist/Planisphere-v$(VERSION)-x86_64.AppImage"
	@rm -rf dist/AppDir
	@echo "==> Linux AppImage created: dist/Planisphere-v$(VERSION)-x86_64.AppImage"

# Package DEB distribution via nfpm
package-deb: build
	@echo "==> Packaging DEB distribution (v$(VERSION))..."
	@command -v nfpm >/dev/null 2>&1 || (echo "==> Installing nfpm..." && go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest)
	@VERSION=$(VERSION) nfpm package --config packaging/nfpm.yaml --packager deb --target dist/
	@echo "==> Linux DEB package created in dist/"

# Package RPM distribution via nfpm
package-rpm: build
	@echo "==> Packaging RPM distribution (v$(VERSION))..."
	@command -v nfpm >/dev/null 2>&1 || (echo "==> Installing nfpm..." && go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest)
	@VERSION=$(VERSION) nfpm package --config packaging/nfpm.yaml --packager rpm --target dist/
	@echo "==> Linux RPM package created in dist/"

# Package Arch Linux package (.pkg.tar.zst) via makepkg
package-arch: package-tarball
	@echo "==> Packaging Arch Linux package (.pkg.tar.zst) via makepkg..."
	@mkdir -p dist/arch-pkg
	@cp packaging/arch/PKGBUILD dist/arch-pkg/
	@cp "dist/planisphere-v$(VERSION)-linux-amd64.tar.gz" dist/arch-pkg/
	@(cd dist/arch-pkg && makepkg -f --nodeps)
	@mv -f dist/arch-pkg/*.pkg.tar.zst dist/
	@rm -rf dist/arch-pkg
	@echo "==> Arch Linux package created in dist/"

# Package Windows distribution (portable zip + NSIS installer)
package-windows: sync-metadata install-frontend
	@echo "==> Building Windows binary and NSIS installer..."
	$(WAILS) build -platform windows/amd64 -nsis -installscope user -webview2 download -trimpath
	@mkdir -p dist
	@echo "==> Packaging Windows distribution files..."
	@tar -a -cf "dist/planisphere-v$(VERSION)-windows-amd64-portable.zip" -C build/bin planisphere.exe 2>/dev/null || \
	 (cd build/bin && zip -q -9 "../../dist/planisphere-v$(VERSION)-windows-amd64-portable.zip" planisphere.exe)
	@echo "==> Windows portable zip created: dist/planisphere-v$(VERSION)-windows-amd64-portable.zip"
	@if [ -f "build/bin/planisphere-amd64-installer.exe" ]; then \
		cp -f "build/bin/planisphere-amd64-installer.exe" "dist/planisphere-v$(VERSION)-windows-amd64-setup.exe"; \
		echo "==> Windows setup installer created: dist/planisphere-v$(VERSION)-windows-amd64-setup.exe"; \
	else \
		echo "Warning: build/bin/planisphere-amd64-installer.exe not found (makensis may not be installed or in PATH)"; \
	fi

clean:
	@echo "==> Cleaning build and distribution directories..."
	rm -rf build/bin/ frontend/dist/ dist/
