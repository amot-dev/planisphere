# Planisphere

Elite Dangerous expedition flight planner, fleet carrier router, and telemetry log.

> [!NOTE]
> This project was developed with help from AI.

---

Like the rest of you, I'm a nerd and I like stats. And while I'm not crazy enough to play Eve Online, I do have a few spreadsheets to my name. Planisphere used to be one of those and it worked wonderfully for me. Sure, it was manual labour planning and filling out the spreadsheet, but I loved it.

Then came the Caspian Explorer. Don't get me wrong, I love the thing, but it also unlocked a new possibility in exploration: figuratively coming home for Christmas. No longer did I have to stand by and watch everyone else participate in events and collect CG rewards. Within a few hours, I could make it back and participate myself.

This fucks the stats.

How do you reconcile tens of thousands of light years tacked onto the stats just from trips back and forth from the bubble? Want to know your average jump range on the expedition and see just how well economical routing did slowing you down? Too bad, because the average is inflated by your dipping of the Caspian in the jet cones for hours.

The solution to that problem was different flight modes with different stats, and even allowing scan data to bleed over (what, you've never stopped to check out an interesting system while buckyballing?) while flight stats stay in isolation. The problem with this solution is it suddenly becomes so tedious to set up that exploration is no longer fun.

That's where Planisphere comes in.


## Features

### Keep your expedition stats pure with flight modes
Separate your flight stats across different styles of transport. If you take a high-speed detour back to the Bubble, switch modes so thousands of buckyballing light years don't ruin your expedition's jump averages and distance logs. You can even use partial filtering to let scan values and exobiology bio-data count toward your wallet totals while keeping the travel mileage strictly isolated.

### Waypoint flight planning without the spreadsheet
Build your flight plan stop by stop with system names, coordinates, and personal notes. Planisphere watches your journal logs in the background; the moment you jump into a destination system, it marks the waypoint as visited and automatically steps forward to the next leg.

### Fleet carrier routing that isn't a chore
Flag waypoints as Carrier Stops. Planisphere queries the Spansh carrier router to plot out 500 Ly jumps with accurate tritium calculations on the fly, tracks your carrier's current location from journal events, and supports manual hop adjustments when you want to chart your own course.

### Real-time cartography and exobiology payouts
Get estimated credit values for bodies you scan (FSS, DSS, first discovery, first mapped, and terraformable bonuses) and plants you sample in your Artemis suit (base species value plus the massive first-logged multiplier) right as they hit your journal.

> [!NOTE]
> **What happens if you die?** Elite Dangerous journals never erase past entries. If your ship is destroyed out in the black, Planisphere preserves all the scans, organic samples, and estimated values you gathered before rebuy. Your stats record the survey work you actually did in the field, even if you never got to cash it in. Respawning also won't inflate your jump distance or advance your waypoints.

### Built-in POI catalog
Automatically flags systems listed in EDAstro's Galactic Exploration Catalog (GEC) and Galactic Mapping Project (GMP). When your route passes a notable landmark or planetary nebula, you get the POI name and one-click shortcuts to EDAstro, Inara, and EDSM.


## Installation

### Arch Linux (AUR)

Planisphere is available on the Arch User Repository (AUR) as [`planisphere-bin`](https://aur.archlinux.org/packages/planisphere-bin):

```bash
yay -S planisphere-bin
# or
paru -S planisphere-bin
```

### Prebuilt Packages

Prebuilt binaries are available under [GitHub Releases](https://github.com/amot-dev/planisphere/releases):
- **Linux**: Standalone AppImage and tarball
- **Windows**: NSIS setup installer and portable zip

### Building from Source

Planisphere builds from source as a native desktop binary using Go, Wails, and React.

#### Prerequisites

- **Go**: 1.26 or newer
- **Node.js**: 20+ or 22+ and `npm`
- **Wails CLI v2**:
  ```bash
  go install github.com/wailsapp/wails/v2/cmd/wails@latest
  ```
- **C compiler and development headers**:
  - **Debian / Ubuntu**: `sudo apt install libgtk-3-dev libwebkit2gtk-4.1-dev build-essential`
  - **Fedora**: `sudo dnf install gtk3-devel webkit2gtk4.1-devel gcc-c++`
  - **Arch Linux**: `sudo pacman -S gtk3 webkit2gtk-4.1 base-devel`
  - **Windows**: [WebView2 runtime](https://developer.microsoft.com/en-us/microsoft-edge/webview2/) (preinstalled on Windows 10/11) and a C compiler (such as `gcc` via MinGW-w64 or [w64devkit](https://github.com/skeeto/w64devkit))

#### Build Steps

1. Clone the repository:
   ```bash
   git clone https://github.com/amot-dev/planisphere.git
   cd planisphere
   ```

2. Build the application:
   ```bash
   make build
   ```

3. Run the application:
   ```bash
   make run
   ```


## Development

### Stack

- **Desktop Framework**: Go + Wails v2 (native WebKitGTK on Linux, WebView2 on Windows)
- **Frontend UI**: React 19, TypeScript, and Tailwind CSS v4
- **Local Storage**: Pure-Go SQLite (`modernc.org/sqlite`, zero CGo database dependencies)
- **Build / Test**: Vite, Vitest, and Go standard testing

### Architecture

- **Local-First**: Everything lives in a local SQLite database on your machine. Your journals, route waypoints, and cached coordinates stay local and work completely offline.
- **Direct Journal Tailing**: Watches the Elite Dangerous journal folder directly using fsnotify. It handles out-of-order writes safely and never touches your game files.
- **Single Source of Truth Config**: Versioning, product name, and metadata live in `app.config.json`. The sync script (`make sync-metadata`, run automatically during builds) keeps `wails.json`, `package.json`, `package-lock.json`, `index.html`, `version.go`, `version.ts`, and package manifests in sync.

### Commands

Planisphere provides Makefile targets to manage development, testing, and packaging:

```bash
# Install frontend dependencies
make install-frontend

# Build desktop binary (build/bin/planisphere)
make build

# Start live development server with hot reload
make dev

# Build and run the desktop application
make run

# Run complete test suite (backend Go tests + frontend Vitest + typecheck)
make test

# Run backend Go tests only
make test-backend

# Run frontend test suite only
make test-frontend

# Run frontend TypeScript typecheck
make typecheck

# Re-sync app.config.json metadata across all targets
make sync-metadata

# Clean build and distribution directories
make clean
```

### Packaging

Distributable packages can be generated locally using the following targets:

```bash
# Package Linux AppImage (dist/Planisphere-v*-x86_64.AppImage)
make package-appimage

# Package Linux tarball (dist/planisphere-v*-linux-amd64.tar.gz)
make package-tarball

# Package Debian (.deb) and RPM (.rpm) packages via nfpm
make package-deb
make package-rpm

# Package Arch Linux package (.pkg.tar.zst) via makepkg
make package-arch

# Package Windows portable zip and NSIS setup installer
make package-windows
```

## Disclaimer

Planisphere is an independent fan-made tool. It is not affiliated with, endorsed by, or sponsored by Frontier Developments plc. Elite Dangerous is a registered trademark of Frontier Developments plc.
