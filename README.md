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
Build your flight plan stop by stop with system names, coordinates, and personal notes. Planisphere watches your journal logs in the background; the moment you jump into a destination system, it marks the waypoint as visited and automatically steps forward to the next leg. During active expeditions, live telemetry displays the remaining distance to your next waypoint and continuously updates remaining route mileage.

### Fleet carrier routing that isn't a chore
Flag waypoints as Carrier Stops. Planisphere queries the Spansh carrier router to plot out 500 Ly jumps with accurate tritium calculations on the fly, tracks your carrier's current location from journal events, and supports manual hop adjustments or logging manual carrier jumps directly from the expedition statistics sidebar even without a scheduled Carrier Stop waypoint.

### Real-time cartography and exobiology payouts
Get estimated credit values for bodies you scan (FSS, session-aware Horizons or Odyssey DSS mapping, terraformable, first discovery, and first mapped bonuses) and plants you sample in your Artemis suit (base species value plus first-logged multiplier) right as they hit your journal. First-bonus estimates automatically exclude populated systems and pre-seeded tag-locked Bubble worlds where discovery tags cannot be claimed.

> [!NOTE]
> **Exobiology first-logged detection ([Frontier Issue #81891](https://issues.frontierstore.net/issue-detail/81891)):** Because Elite Dangerous journals do not reliably populate `WasLogged` on `ScanOrganic` events when another commander has already logged a species, Planisphere works around this by checking `WasFootfalled == false` on the parent body's `Scan` event (falling back to unmapped and undiscovered checks on pre-2025 journals that predate `WasFootfalled`) to estimate first-logged bonuses and detect stolen exobiology bonuses. As a tradeoff, if another commander previously achieved First Footfall on a body without scanning its organics, those samples are initially estimated at base value and automatically upgraded to include the first-logged bonus once sold at Vista Genomics.

### Realized earnings and loss accounting
When you dock and sell exploration data, Planisphere tracks your actual net realized earnings alongside theoretical survey totals. It automatically logs credit deductions and losses resulting from death (distinguishing on-foot suit or SRV deaths, which only lose unsold exobiology samples in your suit while preserving shipboard cartography, from ship destruction, which loses both), first-discovery or first-logged bonuses claimed first by other commanders, the 25 percent Fleet Carrier Universal Cartographics tariff, and active NPC crew wage deductions.

> [!NOTE]
> **What happens if you die?** Elite Dangerous journals never erase past entries. Whether you die on foot or lose your ship out in the black, Planisphere preserves all the scans, organic samples, and theoretical estimated values you gathered before respawning, while recording unsold suit exobiology samples (on any death) and shipboard cartography (on ship destruction) as death losses under realized accounting. Because death losses are classified from the journal's `Resurrect` event, if you can afford your ship's insurance (`Bankrupt: false`) but voluntarily forfeit your ship for a free starter Sidewinder and the journal logs `Option: "free"` with `Cost: 0` instead of `"freewinder"`, that respawn cannot be distinguished from a free on-foot redeployment to orbit. Respawning also won't inflate your jump distance or advance your waypoints.

### Optional historical import from EDDiscovery
If your older raw journal files have been purged or archived, Planisphere can optionally recover your flight history directly from an existing EDDiscovery database (`EDDUser.sqlite`) on an as-needed basis during setup or through Settings.

### Built-in POI catalog
Automatically flags systems listed in EDAstro's Galactic Exploration Catalog (GEC) and Galactic Mapping Project (GMP). When your route passes a notable landmark or planetary nebula, you get the POI name and one-click shortcuts to EDAstro, Inara, and EDSM.


## Valuation and Realization Design

### What Realized Value Represents
`Realized Value` tracks **net credits pocketed** by the commander after all station tariffs and crew cuts:
- **Universal Cartographics** (`SellExplorationData` and `MultiSellExplorationData`) logs `TotalEarnings` already net of both the 25 percent Fleet Carrier tariff and active NPC crew wage shares.
- **Vista Genomics** (`SellOrganicData`) logs gross `Value + Bonus` before crew deductions, so Planisphere subtracts matched `NpcCrewPaidWage` events logged within five seconds of the sale.

Every subline displayed beneath `Realized Value` (`Cartographics Lost (Death)`, `Exobiology Lost (Death)`, `Cartographics Stolen`, `Exobiology Stolen`, `Carrier Tariffs`, and `NPC Crew Payouts`) is an **informational breakdown of deductions and unrealized losses already absent from `Realized Value`** rather than an additional fee to subtract again. When all data has been sold, `Realized Value` stays at or below `(Cartographics Value + Exobiology Value) - Sum(Sublines)` up to minor cartography formula variance; any larger gap represents unsold pending scans still aboard your ship or suit.

### Community Goal and Historical Era Exceptions
Permanent, current-era game constants live in `internal/ed/` (including `Radicoida Unica` at its permanent `119,037 Cr` standard base value), while time-bounded and station-bounded pricing overrides are isolated in `internal/ed/exceptions/`:
- **Historical Era Pricing (`era_pre_update14.go`)**: Prior to Update 14 (`2022-11-29T08:00:00Z`), Vista Genomics used lower legacy species base values and a `2x` (`1x` bonus) first-logged multiplier instead of the modern `5x` (`4x` bonus) multiplier. Because Frontier evaluates exobiology payouts under the table active at redemption time rather than scan time, era rules evaluate against the sale timestamp when sold (so pre-Update-14 samples sold after Update 14 receive modern payouts) and against the scan timestamp when unsold or lost to death.
- **Community Goal Overrides (`cg_*.go`)**: Temporary event multipliers are maintained as separate per-event files added post-event once exact parameters are verified. For example, `cg_2025_11_hip87621.go` models the November 2025 `HIP 87621` Exobiology Initiative (`8x` base value = `952,296 Cr` per `Radicoida Unica` sample sold at `Exogene Sciences`), paired with multi-sample batch consumption when Vista Genomics aggregates repeated scans of the same species into a single `BioData` sale entry.

### Known Limitations and Potential Inaccuracies
Several journal edge cases cannot be reconstructed deterministically from Frontier's logs and are intentionally left unguessed to avoid introducing phantom losses:
- **Uncurated Community Goals**: Live or historical Community Goals without an entry in `internal/ed/exceptions/` use standard species or body estimates until a post-event rule file is added, while realized totals still record actual net credits paid.
- **Bulk `MultiSellExplorationData` opacity and cartography rounding**: Unlike single-system `SellExplorationData` (which lists `Discovered` bodies), bulk `MultiSellExplorationData` bakes first-discovery and first-mapped bonuses directly into `BaseValue` and reserves `Bonus` strictly for flat system-completion bonuses. Because per-body discovery flags are omitted on bulk pages, Planisphere cannot detect stolen discovery bonuses on multi-system sales and does not infer losses from `Estimated - BaseValue`, which naturally varies by roughly 1 to 2 percent due to mass-curve rounding and passive star or asteroid-belt honks without `Scan` events.
- **Pre-death first-logged bonuses on previously footfalled worlds**: On a body where an earlier visitor already set `WasFootfalled: true` without sampling every species, the journal provides no per-species `WasLogged` truth prior to sale. If you die before selling at Vista Genomics, lost samples on that body can only be valued at base species value (`1x`) rather than `5x`.
- **Unlogged Vista Genomics NPC crew cuts**: Frontier occasionally deducts an active NPC pilot's cut from Vista Genomics payouts without writing an `NpcCrewPaidWage` journal event. Planisphere only deducts explicitly logged `NpcCrewPaidWage` events within five seconds of a sale rather than estimating cuts from `Statistics` ledger deltas, which blend combat, trade, and exploration crew wages across sessions.


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
- **Direct Journal Tailing**: Watches the Elite Dangerous journal folder directly using native filesystem notifications (`fsnotify`) as its primary event pipeline, with a configurable debounce window (`0.3s` to `30s`, `2s` default) to coalesce rapid write bursts such as FSS body scans. It handles out-of-order writes safely and never touches your game files.
  > [!WARNING]
  > The journal folder must reside on a local drive and exist prior to starting Planisphere. Remote network shares (NFS, SMB/CIFS) and non-existent folders do not emit kernel filesystem events and are not supported for live log streaming.
- **Optional Historical Ingestion**: When older journal files are no longer available on disk, Planisphere can optionally backfill past flight events from an existing EDDiscovery database (`EDDUser.sqlite`).
- **Single Source of Truth Config**: Versioning, product name, and metadata live in `app.config.json`. The sync script (`make sync-metadata`, run automatically during builds) keeps `wails.json`, `package.json`, `package-lock.json`, `index.html`, `version.go`, `version.ts`, and package manifests in sync.

### Commands

Planisphere provides Makefile targets to manage development, testing, and packaging:

```bash
# Install frontend dependencies
make install-frontend

# Build desktop binary (build/bin/planisphere)
make build

# Build and run the debug desktop binary using an isolated temporary database snapshot
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
