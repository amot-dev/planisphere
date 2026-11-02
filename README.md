# Planisphere

Elite Dangerous expedition flight planner, fleet carrier router, and telemetry log.

> [!NOTE]
> This project was developed with help from AI.

---

Like the rest of you, I'm a nerd and I like stats. And while I'm not crazy enough to play Eve Online, I do have a few spreadsheets to my name. Planisphere used to be one of those and it worked wonderfully for me. Sure, it was manual labour planning and filling out the spreadsheet, but I loved it.

Then came the Caspian Explorer. Don't get me wrong, I love the thing, but it also unlocked a new possibility in exploration: figuratively coming home for Christmas. No longer did I have to stand by and watch everyone else participate in events and collect CG rewards. Within a few hours, I could make it back and participate myself.

This fucks the stats.

How do you reconcile tens of thousands of light years tacked onto the stats just from trips back and forth from the bubble? Want to know your average jump range on the expedition and see just how well economical routing did slowing you down? Too bad, because the average is inflated by your dipping of the Caspian in the jet cones for hours.

The solution to that problem was different travel modes with different stats, and even allowing scan data to bleed over (what, you've never stopped to check out an interesting system while buckyballing?) while travel stats stay in isolation. The problem with this solution is it suddenly becomes so tedious to set up that exploration is no longer fun.

That's where Planisphere comes in.


## Features

### Keep your expedition stats pure with travel modes
Separate your travel stats across different styles of transport. If you take a high-speed detour back to the Bubble, switch modes so thousands of buckyballing light years don't ruin your expedition's jump averages and distance logs. You can even use partial filtering to let scan values and exobiology bio-data count toward your wallet totals while keeping the travel mileage strictly isolated.

### Waypoint flight planning without the spreadsheet
Build your flight plan stop by stop with system names, coordinates, and personal notes. Planisphere watches your journal logs in the background; the moment you jump into a destination system, it marks the waypoint as visited and automatically steps forward to the next leg.

### Fleet carrier routing that isn't a chore
Flag waypoints as Carrier Stops. Planisphere queries the Spansh carrier router to plot out 500 Ly jumps and calculate tritium consumption on the fly, with support for manual hop adjustments when you want to chart your own course.

### Real-time cartography and exobiology payouts
Get estimated credit values for bodies you scan (FSS, DSS, first discovery, first mapped, and terraformable bonuses) and plants you sample in your Artemis suit (base species value plus the massive first-logged multiplier) right as they hit your journal.

> [!NOTE]
> **What happens if you die?** Elite Dangerous journals never erase past entries. If your ship is destroyed out in the black, Planisphere preserves all the scans, organic samples, and estimated values you gathered before rebuy. Your stats record the survey work you actually did in the field, even if you never got to cash it in. Respawning also won't inflate your jump distance or advance your waypoints.

### Built-in POI catalog
Automatically flags systems listed in EDAstro's Galactic Exploration Catalog (GEC) and Galactic Mapping Project (GMP). When your route passes a notable landmark or planetary nebula, you get the POI name and one-click shortcuts to EDAstro, Inara, and EDSM.


## Installation

### Building from Source

Planisphere currently builds from source as a native desktop binary.

#### Prerequisites

- Go
- Node.js and npm
- Wails CLI (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`)
- C compiler and development headers:
  - **Debian / Ubuntu**: `libgtk-3-dev libwebkit2gtk-4.1-dev build-essential`
  - **Fedora**: `gtk3-devel webkit2gtk4.1-devel gcc-c++`
  - **Arch**: `gtk3 webkit2gtk-4.1 base-devel`
  - **Windows**: Webview2 runtime and a C compiler (such as gcc via w64devkit)

#### Build Steps

1. Clone the repository:
   ```bash
   git clone https://github.com/your-username/planisphere.git
   cd planisphere
   ```

2. Install frontend dependencies:
   ```bash
   cd frontend && npm install && cd ..
   ```

3. Build the binary:
   ```bash
   wails build -tags webkit2_41
   ```
   The executable will be generated at `build/bin/planisphere`.

4. Run the app:
   ```bash
   ./build/bin/planisphere
   ```

### Prebuilt Packages

Standalone releases (Linux AppImages and Windows installers) will be published under GitHub Releases so you can grab a binary without needing Go or Node installed.


## Development

### Stack

- **Desktop Framework**: Go + Wails (native WebKit window, no Electron resource hogging)
- **Frontend UI**: React, TypeScript, and Tailwind CSS
- **Local Storage**: SQLite
- **Build / Test**: Vite and Vitest

### Architecture

- **Local-First**: Everything lives in a local SQLite database on your machine. Your journals, route waypoints, and cached coordinates stay local and work completely offline.
- **Direct Journal Tailing**: Watches the Elite Dangerous journal folder directly. It handles out-of-order writes safely and never touches your game files.
- **Single Source of Truth Config**: Versioning, product name, and metadata live in `app.config.json`. The sync script (`npm run sync-metadata` or any build) keeps `wails.json`, `package.json`, `index.html`, `go.mod`, and version constants in sync.

### Commands

```bash
# Build desktop binary (build/bin/planisphere)
make build

# Run complete test suite (backend Go tests + frontend Vitest + typecheck)
make test

# Start live development with hot reload
make dev

# Re-sync app.config.json metadata
make sync-metadata
```


## Improvements & Future Ideas

### Possible Improvements
- **Expedition Table Virtualization (Option B)**: The expedition flight planner currently relies on native CSS scrolling combined with engine-level `content-visibility: auto; contain-intrinsic-size: 44px;` (Option A) for zero JavaScript execution during scrolling and smooth 60fps performance on typical journeys. As a possible future enhancement if support for massive expeditions (>100 waypoints) is ever needed, isolated, memoized windowed virtualization with localized cell state (Option B) can be introduced to cap active DOM nodes without triggering parent-level table re-renders on scroll.

### Roadmap Features
- **Death / "Hardcore" Telemetry Toggle**: A setting for commanders who want the harsh in-game reality reflected in their wallet stats. When enabled, dying before docking at a station or carrier would zero out or mark un-cashed cartography and exobiology payouts as lost in action, while still preserving the flight logs of where you went.
- **Prebuilt Releases**: Packaged Linux AppImages and Windows installers distributed directly via GitHub Releases, removing the need to install Go, Node, or C compiler headers.
- **Expedition Log Exports**: Export completed expedition stats, visited waypoints, and discoveries into clean markdown reports, flight log summaries, or CSV sheets.


## Disclaimer

Planisphere is an independent fan-made tool. It is not affiliated with, endorsed by, or sponsored by Frontier Developments plc. Elite Dangerous is a registered trademark of Frontier Developments plc.
