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
When an expedition spans months, you don't always fly the same way. Flight Modes let you isolate your stats per expedition leg depending on what you're doing in the cockpit.

- **Per-Expedition Leg Modes:** Each expedition tracks its own active flight mode in the planner header (defaulting to `Travel` on creation and hidden once completed) and stamps each visited leg independently so overlapping expeditions never interfere with one another. Switch modes when you take a high-speed detour back to the Bubble so neutron highway jumps don't skew your expedition's average jump range or distance totals.
- **Partial Filtering:** Toggle any mode in the sidebar Telemetry Filter between full inclusion, scan-only inclusion, or complete exclusion. Scan-only mode lets cartography and exobiology discoveries count toward your wallet while keeping travel mileage isolated.
- **Per-Leg and Cascade Editing:** Reassign flight modes on any individual visited or in-progress leg from its telemetry drawer, or apply a flight mode from any visited leg onward across all subsequent visited legs to reclassify multi-phase routes in a few clicks without changing your active flight mode.
- **Custom Flight Modes:** Create your own flight modes in Config with custom names, descriptions, and colour tags. Deleting a custom mode prompts for confirmation with a breakdown of affected expeditions and leg counts before reassigning them back to `Travel`.

### Expedition Planning
Plan your route stop by stop without juggling external spreadsheets.

- **Multiple Expeditions, Bulk Import, and Cloning:** Create and switch between multiple expeditions, optionally bulk-import waypoints from a pasted star system list or a `.txt` / Spansh `.csv` file when creating a new expedition (automatically resolving known local coordinates and cached GEC POIs), or clone an existing expedition (preserving its waypoints and region headers) into a fresh expedition.
- **Waypoints and Region Headers:** Build your expedition with system names, coordinates, and personal notes, and organize stops with Region Headers that automatically suggest the region matching your next waypoint and prioritize adjacent regions.
- **Coordinate Resolution and Visit History:** Waypoint coordinates resolve from your local journal history first and fall back to EDSM for unvisited systems. The status indicator shows the system's galactic region, coordinates, and how many times you have visited it (`Never Visited` via EDSM or visit count via Journal), automatically switching from EDSM to Journal and incrementing your visit count when you jump into the system.
- **Built-in POI Catalog and External Links:** Systems matching EDAstro's Galactic Exploration Catalog (GEC) and Galactic Mapping Project (GMP) are flagged automatically alongside configurable one-click waypoint links to Inara, EDSM (defaulting to `If No GEC`), and GEC.
- **Flexible Expedition Start:** Engage an expedition from a recorded historical visit to Waypoint #1 and choose the starting flight mode applied to any already-visited systems and live flight (retroactively replaying all subsequent journal events across your route). Expedition telemetry, duration, and Fleet Carrier weekly upkeep begin upon your first departure from Waypoint #1. Complete an expedition to lock its end timestamp to your final visited waypoint's last in-system exploration activity (falling back to arrival if none), or reopen it if you closed it early by mistake.
- **Sequential Automatic Progression:** Planisphere watches your journal in the background. When you jump into the immediate next planned destination system, that waypoint is marked visited and remains your single live drawer while you explore the system; once you depart for another star system, the planner advances to the next leg, updating remaining distance to the next stop and total remaining route mileage in real time.
- **Chronologically Bounded Route Editing:** Reorder, insert, or remove waypoints on the fly during an active expedition, slotting missed historical stops into matching journal arrival windows or moving stops back into the unvisited route plan.
- **Per-Leg Telemetry Drawer:** Expand any visited waypoint or the active next waypoint to inspect that leg's in-system time, full-stay elapsed duration, vessels jumped, distance split between ship FSD and carrier passenger travel, hyperspace jumps, scans (including exploration collected inside the waypoint system itself), and leg-specific estimated vs. realized earnings. Expanding Waypoint #1 displays your pre-expedition historical baseline through departure from Waypoint #1.
- **Live Sync Pause:** Click the top-bar status indicator (`READY`, `BUSY`, `PAUSING`, or `PAUSED`) at any time to pause live journal ingestion and background updates while keeping route editing, configuration changes, and manual imports responsive. New journal writes are queued without disk or database churn and flagged on `PAUSED` until you click to resume and ingest them in chronological order.

### Fleet Carrier Routing
Whether you're taking your own carrier into the black, flying with a squadron, or hitching a ride, carrier jumps are built into the planner.

- **Interactive Spansh Router:** Flag any waypoint as a Carrier Stop to plot 500 Ly hops with per-jump and total tritium calculations, `[ICY RING]` mining indicators on intermediate systems, live fuel depot tracking, and a one-click depot refill button that recalculates remaining fuel costs.
- **Personal and Squadron Carriers:** Supports both Personal Carriers (`25,000 t` capacity) and Squadron Carriers (`60,000 t` capacity) when plotting routes and calculating tritium burn.
- **Manual Jump Logging:** Switch to manual mode or log unscheduled carrier jumps straight from the telemetry sidebar even without a scheduled Carrier Stop waypoint.
- **Expedition Carrier Cost Breakdown:** Track overall Fleet Carrier operating costs for an expedition under **Carrier Cost** (`Tritium Cost`, `Jump Maintenance`, and `Weekly Upkeep`), with estimated values prefixed by `~` and per-expedition vessel classification and weekly upkeep settings.

### Exploration Valuation and Realized Earnings
See what your discoveries are worth as you scan them, and track what actually hits your bank account when you cash in.

- **Live Cartography Estimates:** Calculates credit values for FSS body scans (`Scan`) and Horizons or Odyssey DSS surface mapping (`SAAScanComplete`) as they happen, including terraformable, First Discovery, First Mapped, and DSS probe efficiency bonuses.
- **Live Exobiology Estimates:** Values organic species sampled in your Artemis suit (`ScanOrganic`), including the First Logged multiplier on untouched worlds.
- **Realized Earnings Breakdown:** Compares your estimated survey value against the net credits you actually pocket at Universal Cartographics and Vista Genomics, breaking down losses from death, stolen first-discovery or first-logged bonuses, the 25 percent Fleet Carrier tariff, and hired NPC crew cuts (active or inactive).
- **Expedition and All-Time Scopes:** Switch the telemetry sidebar between your active expedition (with flight mode filters) and all-time career totals across your entire journal archive, with travel distance broken down between ship FSD jumps and carrier passenger rides.

### Expedition Export
Share your finished or in-progress journeys with crisp cockpit telemetry cards, offline HTML reports, and complete JSON data archives.

- **Export Studio and Cart Queue:** Open Export Studio from the planner header on any departed or completed expedition to build a custom queue of `Summary`, `Details`, `Waypoints`, and `Highlights` blocks with live preview, a one-click `Quick Share` preset, and sticky checkboxes for queueing rapid variations.
- **Flexible Scopes and Mode Grouping:** Scope `Summary` and `Details` blocks to the full expedition, pre-expedition baseline, a specific leg, all legs, a galactic region, or all galactic regions. When a scope spans multiple active flight modes, toggle cleanly between `Combined` and `Separate` outputs; on `Summary`, include optional inline career diff badges showing your pre-expedition baseline and percentage growth.
- **Per-Data-Point Details and Leaderboard Highlights:** Customize six focused `Details` sections (Flight Operations, Carrier Operations, Exploration Value, Cartographics, Exobiology, and Realized Value) with individual metric checkboxes, queue all applicable `Details` sections at once with default metrics via `All (1 per section)`, or surface `Top 1`, `Top 3`, or `Top 5` leg and waypoint leaderboards in `Highlights`.
- **Self-Contained PNG, Offline HTML, and Complete JSON Outputs:** Independently toggle 2x DPI PNG cards, a single-file print-ready offline `.html` report, and a complete unfiltered `.json` snapshot (always including full ordered highlight rankings). Single-file exports save directly while multi-file exports bundle cleanly into a `.zip` archive.


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

- **Journal Directory:** Point Planisphere to your Elite Dangerous journal directory on first launch. Planisphere ingests your existing `Journal.*.log` files and watches the directory for live events while you fly.
- **Optional EDDiscovery Backfill:** If your older raw journal files have been purged or archived, Planisphere can optionally recover past flight history directly from an existing EDDiscovery database (`EDDUser.sqlite`) during setup or later through Config.

> [!WARNING]
> The Elite Dangerous journal directory must reside on a local drive and exist prior to starting Planisphere. Remote network shares (NFS, SMB/CIFS) and non-existent directories do not emit kernel filesystem notifications and are not supported for live telemetry.

### Updates

Planisphere checks GitHub Releases for newer versions on startup, even when **Online Services** is turned off in Config.


## Design Choices

### Estimated vs. Realized Value
Planisphere tracks two separate credit totals: what your scans were worth in theory when you collected them (**Cartographics Value** and **Exobiology Value**), and the **net credits pocketed** after station tariffs and crew cuts (**Realized Value**):

- **Scan-Time Leg and Mode Attribution:** When you sell exploration data, the realized earnings and any associated deductions (tariffs, crew wages, stolen bonuses, or death losses) are attributed back to the **expedition leg and flight mode where each scan was originally taken**, rather than dumping the entire payout into whichever leg or mode is active when you dock at a station. (Sales that have no matching prior scan in the database fall back to the sale timestamp.)
- **Universal Cartographics** (`SellExplorationData` and `MultiSellExplorationData`) logs `TotalEarnings` already net of both the 25 percent Fleet Carrier tariff and hired NPC crew wage shares (active or inactive).
- **Vista Genomics** (`SellOrganicData`) logs gross `Value + Bonus` before crew deductions, so Planisphere subtracts matched `NpcCrewPaidWage` events logged within five seconds of the sale.

Every subline displayed beneath `Realized Value` (`Cartographics Lost (Death)`, `Exobiology Lost (Death)`, `Cartographics Stolen`, `Exobiology Stolen`, `Carrier Tariffs`, and `NPC Crew Payouts`) is an **informational breakdown of deductions and unrealized losses already absent from `Realized Value`**, not an additional fee to subtract again. Once all data has been sold, `Realized Value` stays at or below `(Cartographics Value + Exobiology Value) - Sum(Sublines)` within 1 to 2 percent due to Elite's internal mass-curve rounding; any larger gap represents unsold pending scans still aboard your ship or suit.

### Death and Respawning
Elite Dangerous journals never erase past entries, and neither does Planisphere. Whether you die on foot or lose your ship out in the black, Planisphere preserves all the scans, organic samples, and theoretical estimated values you gathered before respawning. Respawning also won't inflate your jump distance or advance your waypoints.

Under realized accounting, unsold data is marked as lost once your `Resurrect` event is logged (rather than while sitting on the `Died` screen), split by how you died:
- **On-Foot, SRV, or Nomad Death:** Dying on foot or crashing a Scarab, Scorpion, or ship-launched Nomad only marks unsold exobiology samples in your suit as lost (`Exobiology Lost (Death)`), while mothership cartography remains pending until sold. Losing an unoccupied parked SRV or Nomad while you are alive on foot does not trigger a `Resurrect` event and causes no data loss.
- **Ship Destruction:** Losing your main ship marks both unsold suit exobiology samples and unsold shipboard cartography as lost.
- **Re-Sampling and Re-Scanning After Death:** Because Elite Dangerous locks the Genetic Sampler for a species on a body until your sample is sold or wiped by death, every completed `ScanOrganic` analysis represents a distinct physical sample collected in-game. If you crash a Nomad or SRV, redeploy to your hovering ship, and re-sample the same species on the same body, both collection attempts count toward `Organic Samples` and `Exobiology Value` (including expected first-logged bonuses), with the destroyed sample offset in `Exobiology Lost (Death)` so `(Exobiology Value - Exobiology Lost (Death))` still reconciles with `Realized Value` upon sale. By contrast, Elite re-emits body `Scan` events freely without death or sale whenever you re-target a body across sessions, so `Cartographics Value` counts each body once while `Cartographics Lost (Death)` and `Realized Value` still track a lost scan and any later re-scan sale.

### Mid-Jump Disconnect Recovery
When Elite Dangerous disconnects during hyperspace (such as an Orange Sidewinder, Mauve Adder, or Taupe Cobra error mid-jump), Frontier's servers still complete the jump to your destination system and count the hop in your lifetime `Statistics`, while the client closes before writing an `FSDJump` event. When you reconnect, your journal logs `LoadGame` followed by an undocked `Location` event out in deep space inside the destination system.

Planisphere recovers these server-completed jumps deterministically rather than dropping your mileage or guessing across missing logs:
- **Verified Hyperspace Initiation:** Whenever a `LoadGame` event in a jump-capable ship is followed by an undocked, in-space `Location` event in a new star system (excluding stations, Fleet Carriers, concourses, SRVs, shuttles, multicrew, and `Resurrect` respawns), Planisphere checks the preceding session window for a hyperspace `StartJump` targeting that exact destination system. When matched, the jump count, exact 3D coordinate distance, waypoint visit count, and leg vessel history are credited automatically. If pre-jump `StartJump` history is absent (for example, when playing across two machines without syncing journal files), Planisphere assumes no jump rather than guessing.
- **Armed FSD Boost Attribution:** Any completed `JetConeBoost` or FSD injection `Synthesis` (`FSD Basic`, `FSD Standard`, or `FSD Premium`) armed during that uninterrupted flight stay (and not cleared by a `ShipyardSwap` or an intervening relog) is credited to the recovered jump.

### Galactic Regions and Coordinate Resolution
- **Deterministic Galactic Region Lookup:** Elite Dangerous omits the galactic region name from `FSDJump`, `Location`, and `CarrierJump` journal events (logging `Region` only inside `CodexEntry` events), and the EDSM system endpoint does not return region names either. Instead, because Elite Dangerous defines its 42 Codex regions on a fixed 2048 by 2048 2D grid across the `(X, Z)` galactic plane at a scale of `83 / 4096` pixels per light year (about `49.35 Ly` per cell), Planisphere resolves each waypoint's galactic region deterministically from its `(X, Z)` coordinates as soon as coordinates are known from either your journal or EDSM.
- **User-Controlled Region Headers:** Even though every resolved waypoint knows its galactic region automatically, Region Header rows in the planner remain under your control rather than auto-inserting on every region crossing. Keeping region headers manual prevents table rows from shifting asynchronously while coordinates resolve in the background, avoids repetitive header spam when a straight-line route skims back and forth across a region border, and supports top-down planning where you lay out region headers as route chapters before choosing specific star systems.
- **Visit Counting and Arrival-Driven Refresh:** A waypoint's coordinate indicator counts completed system entries (`FSDJump`, `CarrierJump`, and verified crash-recovered hyperspace jumps, with a minimum of `1 Visit` when a system was only logged via `Location` on session start) so logging out and back in at a parked station or carrier does not inflate your visit count with repeated `Location` events. Rather than re-querying visit history on every body scan or organic sample, Planisphere refreshes waypoint visit counts and upgrades EDSM indicators to Journal only when arrival events (`FSDJump`, `Location`, or `CarrierJump`) are ingested or commander filters change.

### Route Progression and Carrier Tracking
- **Sequential Waypoint Progression and Loop-Route Safety:** During an active expedition, Planisphere only automatically completes the immediate next unvisited waypoint when a journal arrival in that system is logged after the previous visited waypoint's timestamp. Jumping into a system listed further down your route does not automatically shuffle or reorder your planned stops. Keeping progression strictly sequential protects loop expeditions, central carrier hub setups, and routes that revisit the same system multiple times from prematurely collapsing future return stops into earlier legs.
- **Waypoint Departure Boundaries and Origin Start:** Each visited waypoint tracks both its arrival timestamp and its first physical departure to another star system:
  - **In-System Drawer Inclusion and Departure-Gated Advancement:** Destination waypoint drawers span from departing the preceding waypoint through departing the destination waypoint, so FSS scans, DSS mappings, and exobiology collected inside a waypoint stay in that waypoint's own drawer and leg elapsed durations add up strictly to the expedition duration. While you are still exploring inside a newly visited waypoint, that waypoint remains your single live (`IN SYSTEM`) drawer and the next waypoint stays queued until you depart.
  - **Origin Waypoint #1 Departure Start:** Expedition telemetry, elapsed duration, and pro-rated Fleet Carrier weekly upkeep begin upon your first departure from Waypoint #1 rather than arrival. Staying in Waypoint #1 for outfitting or staging stays inside Waypoint #1's pre-expedition Historical Baseline, preventing idle staging inflation and eliminating double-counting when chaining expeditions from a shared hub. If you start a standalone expedition in an unexplored deep-space system whose discoveries you want counted inside the expedition, set the immediately prior system as Waypoint #1 instead.
  - **First-Departure Cap and Final Waypoint Completion:** A waypoint's stay closes on your first departure out of that system so later detours or loop returns never sweep unrelated travel back into an earlier drawer. Completing an expedition locks its end timestamp to the final visited waypoint's last in-system exploration activity (`Scan`, `SAAScanComplete`, or `ScanOrganic` analysis, excluding asteroid belt clusters) within that first stay, falling back to arrival if none occurred. If you leave the final waypoint for a side-trip and return to keep scanning before completing, add a repeat waypoint row for the return visit so both stays are captured.
  - **Pre-Entry Waypoints for Flight Mode Switches:** Because a waypoint's in-system exploration inherits its incoming leg's Flight Mode, if you sprint in `Travel` mode toward a POI you intend to survey under `Exploration`, place a waypoint in the system immediately prior to the POI so you can switch modes before entering it.
- **Chronologically Bounded Waypoint Reordering:** During an active expedition, you can insert, reorder, or delete waypoints on the fly, except Origin Waypoint #1 and any active Spansh carrier route target. A waypoint's star system locks once visited, marked `ARRIVED` as a carrier stop, or targeted by an active Spansh route, leaving POI names and notes editable. Holding or dragging a reorder handle evaluates candidate slots against expedition journal arrivals:
  - **Historical Visited Slots:** A waypoint can be dropped between two visited stops (or immediately after the last visited stop to log a newly visited system) whenever your journal recorded an arrival in that star system within that exact time window, binding the earliest arrival in that window as the waypoint's logged visit timestamp.
  - **Unvisited Route Slots:** Dropping a waypoint anywhere below all visited stops clears its visit timestamp and places it in the unvisited route plan. Moving the last visited waypoint beyond the immediate next unvisited stop unvisits it cleanly without immediately re-triggering sequential evaluation.
- **Fleet Carrier Arrival Frontier and Stop Ordering:** Fleet Carrier stops (`PENDING` and `ARRIVED`) follow the carrier arrival frontier (the latest `ARRIVED` stop) and any open carrier route rather than locking when your ship jumps ahead:
  - You can add a carrier stop after the last `ARRIVED` stop (even if your ship already visited that system) and remove any `PENDING` stop; while an active carrier route is open, carrier stop changes before a manual route target or at or before a Spansh route target stay locked.
  - Reordering never places a `PENDING` stop above an `ARRIVED` stop or an active Spansh route target (blocked in orange as `FC STOP ORDER BLOCKED`), while dragging an `ARRIVED` stop below a `PENDING` stop prompts for confirmation before resetting its status to `PENDING`.
  - If stop status diverges from your in-game carrier, clicking an unlocked `ARRIVED` or `PENDING` badge during an active expedition opens an override confirmation that cascades across adjacent stops to keep the frontier contiguous.
- **Fleet Carrier Tritium Calculation vs. Spansh:** When plotting a carrier route, Planisphere uses Spansh to find the 500 Ly route hops, but discards Spansh's native tritium estimates. Spansh calculates jump fuel assuming an artificially low amount of tritium in the carrier's depot tank, which under-reports actual in-game fuel burn because depot fuel adds mass to the carrier. Planisphere recalculates every hop using Elite's in-game fuel formula (`round(5 + distance * (usedCapacity + depotTritium + 25000) / 200000)`) with your actual depot level (up to `1,000 t`), updating remaining hop costs as the depot drains or when you click **Refill**.
- **Carrier Passenger Travel vs. Carrier Operations:** Elite Dangerous only writes a `CarrierJump` event to your journal when your commander is docked aboard the carrier during the jump, missing remote carrier jumps ordered from your ship or jumps made by a Squadron Carrier while you are elsewhere. To prevent missing remote jumps or double-counting when you are aboard:
  - **By Carrier (Passenger)** under `Distance Travelled (Ly)` tracks distance from `CarrierJump` journal events when your commander personally rode aboard a carrier, and obeys flight mode filters.
  - **Expedition Carrier Operations** (Carrier Dist, Carrier Jumps, and Tritium) tracks jumps logged through the carrier router and manual jump logger, independent of flight mode filters.
  - **All-Time Personal Carrier Operations** reads your owned carrier's lifetime jump count and distance directly from Frontier's `Statistics` journal event (`FLEETCARRIER_TOTAL_JUMPS` and `FLEETCARRIER_DISTANCE_TRAVELLED`).
- **Shuttle and Multicrew Jump Filtering:** Telepresence (`Multicrew: true`) events are ignored completely across system arrivals, departures, live player coordinates, system visit counts, piloted jumps, distance, boosts, and per-leg vessel history because your physical commander never relocates. Apex Interstellar shuttle (`Taxi: true`) transfers physically move your commander, so they still count as system arrivals, departures, player location, and visit history while excluding shuttle hops from `Hyperspace Jumps`, `By Ship (FSD)`, `Distance Travelled (Ly)`, and per-leg vessel history.
- **Static Expedition Carrier Upkeep:** Expedition carrier weekly upkeep is configured as a single static weekly rate per expedition (continuously pro-rated across the expedition's elapsed duration) rather than tracking mid-expedition service activations or suspensions. Because Fleet Carrier services can only be installed or removed at Carrier Administration systems in the Bubble or Colonia, a carrier's weekly upkeep rate remains fixed while out in deep space, avoiding unnecessary complexity and scope creep.
- **All-Time Carrier Cost and Tritium Omissions:** On the **All Time** tab, both Tritium consumed and **Carrier Cost** are omitted from `Personal Carrier Operations`:
  - Frontier's `Statistics` journal event only reports lifetime jump count and distance (`FLEETCARRIER_TOTAL_JUMPS` and `FLEETCARRIER_DISTANCE_TRAVELLED`), omitting lifetime tritium used, carrier commissioning date, and lifetime upkeep paid.
  - Individual `CarrierJump` events omit fuel used, miss remote jumps made while your commander is not docked aboard, and include passenger rides aboard other commanders' carriers.

### Valuation Rules, Historical Eras, and Community Goals
- **Populated Systems and Tag-Locked Bubble Worlds:** In inhabited systems (`Population > 0`) in the Bubble and Colonia, Elite Dangerous disables First Discovered, First Mapped, and First Footfall tags in-game, yet still writes `WasDiscovered: false` and `WasFootfalled: false` in journal `Scan` events. Pre-seeded Bubble bodies can also log `WasDiscovered: false` alongside `WasMapped: true` (impossible in normal exploration since a body must be discovered to be DSS-mapped). Planisphere filters out populated systems and tag-locked bodies so flying through the Bubble does not inflate your stats with impossible first-discovery bonuses.
- **Session-Aware Horizons vs. Odyssey Mapping Payouts:** Odyssey adds a 30 percent bonus to Detailed Surface Scanner (`SAAScanComplete`) mapping payouts compared to Horizons. Planisphere tracks your `LoadGame` state (`Odyssey: true` or `false`) across your journal timeline so historical Horizons mapping scans are valued under the Horizons formula while Odyssey sessions receive the 30 percent Odyssey bonus.
- **Pre-Update 14 Exobiology:** Prior to Update 14 (`2022-11-29T08:00:00Z`), Vista Genomics used lower legacy species base values and a `2x` (`1x` bonus) First Logged multiplier instead of the modern `5x` (`4x` bonus) multiplier. Because Frontier evaluates exobiology payouts under the table active at redemption time rather than scan time, Planisphere evaluates era rules against the sale timestamp when sold (so pre-Update-14 samples sold after Update 14 receive modern payouts) and against the scan timestamp when unsold or lost to death.
- **Community Goals:** Temporary event overrides are **not** detected automatically. They must be manually added in code and shipped in an update after an event concludes and its exact payout parameters are verified. For example, Planisphere includes a dedicated rule for the November 2025 `HIP 87621` Exobiology Initiative (`8x` base value = `952,296 Cr` per `Radicoida Unica` sample sold at `Exogene Sciences`, paired with multi-sample batch matching when Vista Genomics aggregates repeated scans of the same species into a single `BioData` entry). For any uncurated live or historical Community Goal, estimated values use standard pricing while `Realized Value` still records the actual net credits paid.

### Elite Dangerous Journal Limitations
Several journal edge cases cannot be reconstructed deterministically from Frontier's logs and are intentionally left unguessed to avoid introducing phantom losses:

- **Exobiology First Logged Detection ([Frontier Issue #81891](https://issues.frontierstore.net/issue-detail/81891)):** Because Elite Dangerous journals do not reliably populate `WasLogged` on `ScanOrganic` events when another commander has already logged a species, Planisphere checks `WasFootfalled == false` on the parent body's `Scan` event (falling back to `!WasDiscovered` and `!WasMapped` on pre-2025 journals that predate `WasFootfalled`) to estimate first-logged bonuses and detect stolen exobiology bonuses. As a tradeoff, if another commander previously achieved First Footfall on a body without sampling its organics, those samples are initially estimated at base value (`1x`) and automatically upgraded to `5x` once sold at Vista Genomics. If you die before selling at Vista Genomics on a previously footfalled world, lost samples can only be valued at base value (`1x`).
- **Stolen Bonuses on Bulk `MultiSellExplorationData` Sales:** Unlike single-system `SellExplorationData` (which lists `Discovered` bodies), bulk `MultiSellExplorationData` bakes First Discovery and First Mapped bonuses directly into `BaseValue` and reserves `Bonus` strictly for flat system-completion bonuses. Because per-body discovery flags are omitted on bulk pages, Planisphere cannot detect stolen discovery bonuses on multi-system sales and does not infer losses from `Estimated - BaseValue`, which naturally varies by 1 to 2 percent due to mass-curve rounding and passive star or asteroid-belt honks without `Scan` events.
- **Unlogged Vista Genomics NPC Crew Cuts:** Frontier occasionally deducts a hired NPC pilot's cut (active or inactive) from Vista Genomics payouts without writing an `NpcCrewPaidWage` journal event. Planisphere only deducts explicitly logged `NpcCrewPaidWage` events within five seconds of a sale rather than estimating cuts from `Statistics` ledger deltas, which blend combat, trade, and exploration crew wages across sessions.
- **Detention Ship Impounds and Unmanned Mothership Rebuys:** When your ship is impounded at a penal facility after an on-foot bounty death, or when an unmanned mothership is destroyed while you are on foot, in an SRV, or piloting a Nomad, Odyssey preserves your shipboard Universal Cartographics data in-game yet still logs `Resurrect` with `Option: "rebuy"` and a positive `Cost` identical to a manned ship destruction. Planisphere marks pending cartography under `Cartographics Lost (Death)` on `rebuy`, and does not retroactively clear death losses if those system names later reappear in `MultiSellExplorationData` (which would erase genuine ship-destruction losses whenever a system is passively honked on a return trip). When that surviving data is sold, the payout still lands in `Realized Value`.
- **Voluntary Starter Sidewinder Forfeit:** Because death losses are classified from the journal's `Resurrect` event, if you can afford your ship's insurance (`Bankrupt: false`) but voluntarily forfeit your ship for a free starter Sidewinder and the journal logs `Option: "free"` with `Cost: 0` instead of `"freewinder"`, that respawn cannot be distinguished from a free on-foot redeployment to orbit.


## Development

### Stack

- **Desktop Framework**: Go + Wails v2 (native WebKitGTK on Linux, WebView2 on Windows)
- **Frontend UI**: React 19, TypeScript, and Tailwind CSS v4
- **Local Storage**: Pure-Go SQLite (`modernc.org/sqlite`, zero CGo database dependencies)
- **Build / Test**: Vite, Vitest, and Go standard testing

### Architecture

- **Event-Subscribed Projection Caches**: Derived read caches register declarative triggers against typed Elite journal events and internal domain mutations on a shared invalidation bus. Journal ingestion, importer commits, and user actions compute typed event sets and invalidate only matching projections and UI domains instead of issuing database-wide refreshes.
- **Debounced Filesystem Watcher**: The journal ingester watches the active journal directory via `fsnotify` with a configurable debounce window (`0.3s` to `30s`, `2s` default) to coalesce rapid write bursts such as FSS body scans and handle out-of-order writes safely.
- **Single Source of Truth Config**: Versioning, product name, repository, and metadata live in `app.config.json`. The sync script (`make sync-metadata`, run automatically during builds) keeps `wails.json`, `package.json`, `package-lock.json`, `index.html`, `version.go`, `version.ts`, the Linux `.desktop` entry, and package manifests in sync.
- **Pausable Live Watcher and Idle Animation Gating**: Toggling `PAUSED` from the top bar suspends `fsnotify` file ingestion at the backend watcher (accumulating modified `.log` paths in memory and holding unsolicited background notifications while keeping user-initiated mutations live), suspends the 60-second galactic clock timer, and flushes queued files chronologically on resume; infinite CSS HUD animations also pause automatically whenever the window is hidden.

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

# Create an isolated, WAL-merged snapshot of the active database
# Optional: OUT=<path> SRC=<path> (or ED_PLANNER_DB=<path>)
make snapshot-db

# Run the live data profiler against a temporary snapshot of the active database
# Optional: RUN=<filter> ITER=<count> SAVE=<file.json> BASELINE=<file.json> SKIP_FRONTEND=1 ED_PLANNER_DB=<path>
make profile

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
