#!/usr/bin/env bash
set -euo pipefail

APP_DIR="${1:-dist/AppDir}"
OUT_IMAGE="${2:-dist/Planisphere-x86_64.AppImage}"

if [ ! -d "$APP_DIR" ]; then
  echo "Error: AppDir not found at $APP_DIR. Run 'make package-appimage' instead." >&2
  exit 1
fi

echo "==> Packaging $OUT_IMAGE using appimagetool..."
TOOL_CACHE_DIR="${HOME}/.cache/appimage"
mkdir -p "$TOOL_CACHE_DIR"

if command -v appimagetool >/dev/null 2>&1; then
  ARCH=x86_64 appimagetool "$APP_DIR" "$OUT_IMAGE"
else
  TOOL="${TOOL_CACHE_DIR}/appimagetool-x86_64.AppImage"
  if [ ! -f "$TOOL" ]; then
    echo "==> Downloading appimagetool..."
    curl -sSL "https://github.com/AppImage/appimagetool/releases/download/continuous/appimagetool-x86_64.AppImage" -o "$TOOL"
    chmod +x "$TOOL"
  fi

  if [ ! -d "${TOOL_CACHE_DIR}/squashfs-root" ]; then
    (cd "$TOOL_CACHE_DIR" && "$TOOL" --appimage-extract >/dev/null)
  fi

  ARCH=x86_64 "${TOOL_CACHE_DIR}/squashfs-root/AppRun" "$APP_DIR" "$OUT_IMAGE"
fi

echo "==> Successfully built AppImage: $OUT_IMAGE"
