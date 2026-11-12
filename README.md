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

### Flight Modes
When an expedition spans months, you don't always fly the same way. Flight Modes let you isolate your stats depending on what you're doing in the cockpit.

- **Separate Travel Profiles:** Switch modes when you take a high-speed detour back to the Bubble so neutron highway jumps don't skew your expedition's average jump range or distance totals.
- **Partial Filtering:** Toggle any mode in the sidebar between full inclusion, scan-only inclusion, or complete exclusion. Scan-only mode lets cartography and exobiology discoveries count toward your wallet while keeping travel mileage isolated.
- **Per-Leg Tracking:** Assign flight modes to individual expedition legs to record how you traveled between each stop. Leg modes can always be changed later, even after a waypoint has been visited.
- **Custom Modes:** Create your own flight modes in Settings with custom names, descriptions, and color tags.

### Expedition Planning
Plan your route stop by stop without juggling external spreadsheets.

- **Multiple Plans and Cloning:** Create and switch between multiple expedition plans, or clone an existing expedition (preserving its waypoints and region headers) into a fresh plan.
- **Waypoints and Galactic Regions:** Build your flight plan with system names, coordinates, and personal notes, and organize stops with Galactic Region headers that automatically suggest the region matching your next waypoint and prioritize adjacent regions.
- **Coordinate Resolution and Visit History:** Waypoint coordinates resolve from your local journal history first and fall back to EDSM for unvisited systems. Because the status badge distinguishes local journal matches from EDSM lookups, you can see while planning whether you have previously visited a system (assuming your journal history is accurate).
- **Built-in POI Catalog:** Systems matching EDAstro's Galactic Exploration Catalog (GEC) and Galactic Mapping Project (GMP) are flagged automatically with one-click links to EDAstro, Inara, and EDSM.
- **Flexible Expedition Start:** Engage an expedition from a recorded historical visit to Waypoint #1 (retroactively replaying all subsequent journal events across your route), arm it to start automatically when your ship or carrier arrives at Waypoint #1, or enter a custom Galactic Standard Time timestamp. Complete an expedition to lock its end timestamp, or reopen it if you closed it early by mistake.
- **Automatic Progression:** Planisphere watches your journal in the background. The moment you jump into a destination system, that waypoint is marked visited and the planner advances to the next leg, updating remaining distance to the next stop and total remaining route mileage in real time.
- **Per-Leg Telemetry Drawer:** Expand any visited waypoint to inspect that leg's elapsed duration, vessels jumped, distance split between ship FSD and carrier passenger travel, hyperspace jumps, scans, and leg-specific estimated vs. realized earnings. Expanding Waypoint #1 displays your pre-expedition historical baseline.

### Fleet Carrier Routing
Whether you're taking your own carrier into the black, flying with a squadron, or hitching a ride, carrier jumps are built into the planner.

- **Interactive Spansh Plotter:** Flag any waypoint as a Carrier Stop to plot 500 Ly hops with per-jump and total tritium calculations, `[ICY RING]` mining indicators on intermediate systems, live tritium depot tracking, and a one-click depot refill button that recalculates remaining fuel costs.
- **Personal and Squadron Carriers:** Supports both Personal Carriers (`25,000 t` capacity) and Squadron Carriers (`60,000 t` capacity) when plotting routes and calculating tritium burn.
- **Manual Jump Logging:** Switch to manual mode or log unscheduled carrier jumps straight from the telemetry sidebar even without a scheduled Carrier Stop waypoint.

### Exploration Valuation and Realized Earnings
See what your discoveries are worth as you scan them, and track what actually hits your bank account when you cash in.

- **Live Cartography Estimates:** Calculates credit values for FSS body scans (`Scan`) and Horizons or Odyssey DSS surface mapping (`SAAScanComplete`) as they happen, including terraformable, First Discovery, and First Mapped bonuses.
- **Live Exobiology Estimates:** Values organic species sampled in your Artemis suit (`ScanOrganic`), including the First Logged multiplier on untouched worlds.
- **Realized Earnings Breakdown:** Compares your estimated survey value against the net credits you actually pocket at Universal Cartographics and Vista Genomics, breaking down losses from death, stolen first-discovery or first-logged bonuses, the 25 percent Fleet Carrier tariff, and active NPC crew cuts.
- **Expedition and All-Time Scopes:** Switch the telemetry sidebar between your active expedition (with flight mode filters) and all-time career totals across your entire journal archive, with travel distance broken down between ship FSD jumps and carrier passenger rides.


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

### First-Launch Setup

Everything in Planisphere is stored in a local SQLite database on your machine: your journals, route waypoints, and cached coordinates stay local and work offline. Planisphere never modifies your game files.

- **Journal Directory:** Point Planisphere to your Elite Dangerous journal folder on first launch. Planisphere ingests your existing `Journal.*.log` files and watches the folder for live events while you fly.
- **Optional EDDiscovery Backfill:** If your older raw journal files have been purged or archived, Planisphere can optionally recover past flight history directly from an existing EDDiscovery database (`EDDUser.sqlite`) during setup or later through Settings.

> [!WARNING]
> The Elite Dangerous journal folder must reside on a local drive and exist prior to starting Planisphere. Remote network shares (NFS, SMB/CIFS) and non-existent directories do not emit kernel filesystem notifications and are not supported for live telemetry.


## Design Choices

### Estimated vs. Realized Value
Planisphere tracks two separate credit totals: what your scans were worth in theory when you collected them (**Cartographics Value** and **Exobiology Value**), and the **net credits pocketed** after station tariffs and crew cuts (**Realized Value**):

- **Scan-Time Leg and Mode Attribution:** When you sell exploration data, the realized earnings and any associated deductions (tariffs, crew wages, stolen bonuses, or death losses) are attributed back to the **expedition leg and flight mode where each scan was originally taken**, rather than dumping the entire payout into whichever leg or mode is active when you dock at a station. (Sales that have no matching prior scan in the database fall back to the sale timestamp.)
- **Universal Cartographics** (`SellExplorationData` and `MultiSellExplorationData`) logs `TotalEarnings` already net of both the 25 percent Fleet Carrier tariff and active NPC crew wage shares.
- **Vista Genomics** (`SellOrganicData`) logs gross `Value + Bonus` before crew deductions, so Planisphere subtracts matched `NpcCrewPaidWage` events logged within five seconds of the sale.

Every subline displayed beneath `Realized Value` (`Cartographics Lost (Death)`, `Exobiology Lost (Death)`, `Cartographics Stolen`, `Exobiology Stolen`, `Carrier Tariffs`, and `NPC Crew Payouts`) is an **informational breakdown of deductions and unrealized losses already absent from `Realized Value`**, not an additional fee to subtract again. Once all data has been sold, `Realized Value` stays at or below `(Cartographics Value + Exobiology Value) - Sum(Sublines)` within 1 to 2 percent due to Elite's internal mass-curve rounding; any larger gap represents unsold pending scans still aboard your ship or suit.

### Death and Respawning
Elite Dangerous journals never erase past entries, and neither does Planisphere. Whether you die on foot or lose your ship out in the black, Planisphere preserves all the scans, organic samples, and theoretical estimated values you gathered before respawning. Respawning also won't inflate your jump distance or advance your waypoints.

Under realized accounting, unsold data at the time of a `Resurrect` event is recorded as a death loss based on how you died:
- **On-Foot or SRV Death:** Only unsold exobiology samples in your suit are marked as lost (`Exobiology Lost (Death)`), while shipboard cartography remains pending until sold.
- **Ship Destruction:** Both unsold suit exobiology samples and unsold shipboard cartography are marked as lost.

### Route Progression and Carrier Tracking
- **Out-of-Order Waypoint Visits:** If you jump into a planned waypoint further down your list before visiting the intermediate stops above it, Planisphere does not ignore the arrival or mark the skipped stops as missed. Instead, it moves the visited waypoint up to become your latest completed leg and shifts the unvisited waypoints down below it so you can still visit them later without breaking your route.
- **Fleet Carrier Tritium Calculation vs. Spansh:** When plotting a carrier route, Planisphere uses Spansh to find the 500 Ly route hops, but discards Spansh's native tritium estimates. Spansh calculates jump fuel assuming an artificially low amount of tritium in the carrier's depot tank, which under-reports actual in-game fuel burn because depot fuel adds mass to the carrier. Planisphere recalculates every hop using Elite's in-game fuel formula (`round(5 + distance * (usedCapacity + depotTritium + 25000) / 200000)`) with your actual depot level (up to `1,000 t`), updating remaining hop costs as the depot drains or when you click **Refill**.
- **Carrier Passenger Travel vs. Carrier Operations:** Elite Dangerous only writes a `CarrierJump` event to your journal when your commander is docked aboard the carrier during the jump, missing remote carrier jumps ordered from your ship or jumps made by a Squadron Carrier while you are elsewhere. To prevent missing remote jumps or double-counting when you are aboard:
  - **By Carrier (Passenger)** under `Distance Travelled (Ly)` tracks distance from `CarrierJump` journal events when your commander personally rode aboard a carrier, and obeys flight mode filters.
  - **Expedition Carrier Operations** (Carrier Dist, Carrier Jumps, and Tritium) tracks jumps logged through the carrier route plotter and manual jump logger, independent of flight mode filters.
  - **All-Time Personal Carrier Operations** reads your owned carrier's lifetime jump count and distance directly from Frontier's `Statistics` journal event (`FLEETCARRIER_TOTAL_JUMPS` and `FLEETCARRIER_DISTANCE_TRAVELLED`).

### Valuation Rules, Historical Eras, and Community Goals
- **Populated Systems and Tag-Locked Bubble Worlds:** In inhabited systems (`Population > 0`) in the Bubble and Colonia, Elite Dangerous disables First Discovered, First Mapped, and First Footfall tags in-game, yet still writes `WasDiscovered: false` and `WasFootfalled: false` in journal `Scan` events. Pre-seeded Bubble bodies can also log `WasDiscovered: false` alongside `WasMapped: true` (impossible in normal exploration since a body must be discovered to be DSS-mapped). Planisphere filters out populated systems and tag-locked bodies so flying through the Bubble does not inflate your stats with impossible first-discovery bonuses.
- **Session-Aware Horizons vs. Odyssey Mapping Payouts:** Odyssey adds a 30 percent bonus to Detailed Surface Scanner (`SAAScanComplete`) mapping payouts compared to Horizons. Planisphere tracks your `LoadGame` state (`Odyssey: true` or `false`) across your journal timeline so historical Horizons mapping scans are valued under the Horizons formula while Odyssey sessions receive the 30 percent Odyssey bonus.
- **Pre-Update 14 Exobiology:** Prior to Update 14 (`2022-11-29T08:00:00Z`), Vista Genomics used lower legacy species base values and a `2x` (`1x` bonus) First Logged multiplier instead of the modern `5x` (`4x` bonus) multiplier. Because Frontier evaluates exobiology payouts under the table active at redemption time rather than scan time, Planisphere evaluates era rules against the sale timestamp when sold (so pre-Update-14 samples sold after Update 14 receive modern payouts) and against the scan timestamp when unsold or lost to death.
- **Community Goals:** Temporary event overrides are **not** detected automatically. They must be manually added in code and shipped in an update after an event concludes and its exact payout parameters are verified. For example, Planisphere includes a dedicated rule for the November 2025 `HIP 87621` Exobiology Initiative (`8x` base value = `952,296 Cr` per `Radicoida Unica` sample sold at `Exogene Sciences`, paired with multi-sample batch matching when Vista Genomics aggregates repeated scans of the same species into a single `BioData` entry). For any uncurated live or historical Community Goal, estimated values use standard pricing while `Realized Value` still records the actual net credits paid.

### Elite Dangerous Journal Limitations
Several journal edge cases cannot be reconstructed deterministically from Frontier's logs and are intentionally left unguessed to avoid introducing phantom losses:

- **Exobiology First Logged Detection ([Frontier Issue #81891](https://issues.frontierstore.net/issue-detail/81891)):** Because Elite Dangerous journals do not reliably populate `WasLogged` on `ScanOrganic` events when another commander has already logged a species, Planisphere checks `WasFootfalled == false` on the parent body's `Scan` event (falling back to `!WasDiscovered` and `!WasMapped` on pre-2025 journals that predate `WasFootfalled`) to estimate first-logged bonuses and detect stolen exobiology bonuses. As a tradeoff, if another commander previously achieved First Footfall on a body without sampling its organics, those samples are initially estimated at base value (`1x`) and automatically upgraded to `5x` once sold at Vista Genomics. If you die before selling at Vista Genomics on a previously footfalled world, lost samples can only be valued at base value (`1x`).
- **Stolen Bonuses on Bulk `MultiSellExplorationData` Sales:** Unlike single-system `SellExplorationData` (which lists `Discovered` bodies), bulk `MultiSellExplorationData` bakes First Discovery and First Mapped bonuses directly into `BaseValue` and reserves `Bonus` strictly for flat system-completion bonuses. Because per-body discovery flags are omitted on bulk pages, Planisphere cannot detect stolen discovery bonuses on multi-system sales and does not infer losses from `Estimated - BaseValue`, which naturally varies by 1 to 2 percent due to mass-curve rounding and passive star or asteroid-belt honks without `Scan` events.
- **Unlogged Vista Genomics NPC Crew Cuts:** Frontier occasionally deducts an active NPC pilot's cut from Vista Genomics payouts without writing an `NpcCrewPaidWage` journal event. Planisphere only deducts explicitly logged `NpcCrewPaidWage` events within five seconds of a sale rather than estimating cuts from `Statistics` ledger deltas, which blend combat, trade, and exploration crew wages across sessions.
- **Voluntary Starter Sidewinder Forfeit:** Because death losses are classified from the journal's `Resurrect` event, if you can afford your ship's insurance (`Bankrupt: false`) but voluntarily forfeit your ship for a free starter Sidewinder and the journal logs `Option: "free"` with `Cost: 0` instead of `"freewinder"`, that respawn cannot be distinguished from a free on-foot redeployment to orbit.


## Development

### Stack

- **Desktop Framework**: Go + Wails v2 (native WebKitGTK on Linux, WebView2 on Windows)
- **Frontend UI**: React 19, TypeScript, and Tailwind CSS v4
- **Local Storage**: Pure-Go SQLite (`modernc.org/sqlite`, zero CGo database dependencies)
- **Build / Test**: Vite, Vitest, and Go standard testing

### Architecture

- **Debounced Filesystem Watcher**: The journal ingester watches the active journal directory via `fsnotify` with a configurable debounce window (`0.3s` to `30s`, `2s` default) to coalesce rapid write bursts such as FSS body scans and handle out-of-order writes safely.
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
