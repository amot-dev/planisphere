package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"planisphere/internal/db"
	"planisphere/internal/ed"
	"planisphere/internal/expedition"
	"planisphere/internal/journal"
	"planisphere/internal/modes"
	"planisphere/internal/stats"
	"planisphere/internal/version"
)

var errNotReady = errors.New("database is not available — check the startup error in Settings")

func (a *App) ready() error {
	if a.db == nil {
		if e := a.getErr(); e != "" {
			return fmt.Errorf("database is not available: %s", e)
		}
		return errNotReady
	}
	return nil
}

// ============================================================================
// Settings & journal ingestion
// ============================================================================

// Settings is the user-configurable application state.
type Settings struct {
	SetupCompleted           bool                `json:"setupCompleted"`
	JournalDir               string              `json:"journalDir"`
	DefaultJournalDir        string              `json:"defaultJournalDir"`
	JournalDirExists         bool                `json:"journalDirExists"`
	OnlineLookups            bool                `json:"onlineLookups"`
	DBPath                   string              `json:"dbPath"`
	DefaultDBPath            string              `json:"defaultDbPath"`
	ActiveExpeditionID       int64               `json:"activeExpeditionId"`
	GECCount                 int                 `json:"gecCount"`
	GECFetchedAt             string              `json:"gecFetchedAt"`
	Commanders               []journal.Commander `json:"commanders"`
	EDDiscoveryDBPath        string              `json:"edDiscoveryDbPath"`
	DefaultEDDiscoveryDBPath string              `json:"defaultEdDiscoveryDbPath"`
	EDDiscoveryDBExists      bool                `json:"edDiscoveryDbExists"`
}

// GetSettings returns the current settings.
func (a *App) GetSettings() (Settings, error) {
	if err := a.ready(); err != nil {
		return Settings{}, err
	}
	dir := a.journalDir()
	cmdrs, _ := journal.ListCommanders(a.db)
	if cmdrs == nil {
		cmdrs = []journal.Commander{}
	}
	defaultDB, _ := db.DefaultPath()
	defaultEDD := journal.DefaultEDDiscoveryDBPath()
	discoveredEDD := journal.DiscoverEDDiscoveryDBPath()
	return Settings{
		SetupCompleted:           a.setupCompleted(),
		JournalDir:               dir,
		DefaultJournalDir:        journal.DefaultJournalDir(),
		JournalDirExists:         journal.DirExists(dir),
		OnlineLookups:            a.onlineEnabled(),
		DBPath:                   a.dbPath,
		DefaultDBPath:            defaultDB,
		ActiveExpeditionID:       a.activeExpeditionID(),
		GECCount:                 a.gec.Count(),
		GECFetchedAt:             a.gec.LastFetched(),
		Commanders:               cmdrs,
		EDDiscoveryDBPath:        discoveredEDD,
		DefaultEDDiscoveryDBPath: defaultEDD,
		EDDiscoveryDBExists:      discoveredEDD != "" || journal.FileExists(defaultEDD),
	}, nil
}

// BrowseDBPath opens a save-file dialog so the user can choose where the SQLite database file should be stored.
func (a *App) BrowseDBPath() (string, error) {
	defaultDir := ""
	defaultFilename := db.DefaultDBFileName
	if a.dbPath != "" {
		defaultDir = filepath.Dir(a.dbPath)
		defaultFilename = filepath.Base(a.dbPath)
	}
	return runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:                fmt.Sprintf("Select %s Database Location", version.ProductName),
		DefaultDirectory:     defaultDir,
		DefaultFilename:      defaultFilename,
		CanCreateDirectories: true,
		Filters: []runtime.FileFilter{
			{DisplayName: "SQLite Database (*.db, *.sqlite)", Pattern: "*.db;*.sqlite"},
		},
	})
}

// SetDatabasePath updates the active SQLite database location, persists it in
// bootstrap.json, and hot-switches the running database connection if changed.
func (a *App) SetDatabasePath(dbPath string) (Settings, error) {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		p, err := db.DefaultPath()
		if err != nil {
			return Settings{}, err
		}
		dbPath = p
	} else if journal.DirExists(dbPath) {
		dbPath = filepath.Join(dbPath, db.DefaultDBFileName)
	}
	dbPath = filepath.Clean(dbPath)
	if dbPath != a.dbPath {
		if err := a.openAndWireDB(dbPath); err != nil {
			return Settings{}, err
		}
	}
	if err := db.SaveConfiguredPath(dbPath); err != nil {
		return Settings{}, err
	}
	a.changed("settings", 0)
	return a.GetSettings()
}

// SetupConfigureJournals sets the journal directory, performs a synchronous
// initial scan of local .log files, starts the live watcher, and returns the
// resulting archive status (including oldest journal date and discovered CMDRs).
func (a *App) SetupConfigureJournals(dir string) (IngestStatus, error) {
	if err := a.ready(); err != nil {
		return IngestStatus{}, err
	}
	dir = strings.TrimSpace(dir)
	if dir != "" && !journal.DirExists(dir) {
		return IngestStatus{}, fmt.Errorf("directory does not exist: %s", dir)
	}
	if err := db.SetSetting(a.db, settingJournalDir, dir); err != nil {
		return IngestStatus{}, err
	}
	a.runScan()
	a.watcher.Start(a.ctx, a.journalDir())
	a.changed("journal", 0)
	return a.GetIngestStatus()
}

// CompleteSetup marks the first-time setup wizard as finished and triggers
// any deferred startup tasks (watcher and GEC catalog sync).
func (a *App) CompleteSetup() (Settings, error) {
	if err := a.ready(); err != nil {
		return Settings{}, err
	}
	if err := db.SetSetting(a.db, settingSetupCompleted, "1"); err != nil {
		return Settings{}, err
	}
	a.watcher.Start(a.ctx, a.journalDir())
	if a.onlineEnabled() && a.gec.Count() == 0 {
		go a.maybeRefreshGEC()
	}
	a.changed("settings", 0)
	return a.GetSettings()
}

// SetCommanderEnabled enables or disables a commander in statistics and plans.
func (a *App) SetCommanderEnabled(name string, enabled bool) (Settings, error) {
	if err := a.ready(); err != nil {
		return Settings{}, err
	}
	if err := journal.SetCommanderEnabled(a.db, name, enabled); err != nil {
		return Settings{}, err
	}
	a.invalidateAllTimeCache()
	a.reevaluateActive()
	a.changed("journal", 0)
	return a.GetSettings()
}

// BrowseEDDiscoveryDB opens a file picker to select an EDDUser.sqlite file.
func (a *App) BrowseEDDiscoveryDB() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select EDDiscovery SQLite Database (EDDUser.sqlite)",
		Filters: []runtime.FileFilter{
			{DisplayName: "SQLite Database (*.sqlite, *.db)", Pattern: "*.sqlite;*.db"},
		},
	})
}

// ImportEDDiscoveryHistory imports raw historical journal events from an
// EDDiscovery SQLite database (EDDUser.sqlite) into our self-contained SQLite
// archive so deleted older .log files are permanently recovered.
func (a *App) ImportEDDiscoveryHistory(eddPath string) (int, error) {
	if err := a.ready(); err != nil {
		return 0, err
	}
	eddPath = strings.TrimSpace(eddPath)
	if eddPath == "" {
		eddPath = journal.DefaultEDDiscoveryDBPath()
	}
	defer a.emitEDDProgress(false, 0, 0, 0, "")
	res, err := a.ingester.ImportEDDiscoveryDB(eddPath, func(cur, tot, files int, stage string) {
		a.emitEDDProgress(stage != "Complete", cur, tot, files, stage)
	})
	if err != nil {
		return 0, err
	}
	_ = db.SetSetting(a.db, "edd_history_recovered_v1", "1")
	a.invalidateAllTimeCache()
	a.reevaluateActive()
	a.changed("journal", 0)
	return res.LinesRead, nil
}

// SetJournalDir changes the journal directory, restarts the watcher and
// performs a catch-up scan in the background. Pass "" to restore the default.
func (a *App) SetJournalDir(dir string) (Settings, error) {
	if err := a.ready(); err != nil {
		return Settings{}, err
	}
	dir = strings.TrimSpace(dir)
	if dir != "" && !journal.DirExists(dir) {
		return Settings{}, fmt.Errorf("directory does not exist: %s", dir)
	}
	if err := db.SetSetting(a.db, settingJournalDir, dir); err != nil {
		return Settings{}, err
	}
	go func() {
		a.runScan()
		a.watcher.Start(a.ctx, a.journalDir())
	}()
	return a.GetSettings()
}

// BrowseJournalDir opens a native directory picker and returns the choice ("" if cancelled).
func (a *App) BrowseJournalDir() (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:            "Select Elite Dangerous journal folder",
		DefaultDirectory: a.journalDir(),
	})
}

// SetOnlineLookups enables/disables EDSM and EDAstro network requests.
func (a *App) SetOnlineLookups(enabled bool) (Settings, error) {
	if err := a.ready(); err != nil {
		return Settings{}, err
	}
	v := "1"
	if !enabled {
		v = "0"
	}
	if err := db.SetSetting(a.db, settingOnline, v); err != nil {
		return Settings{}, err
	}
	if enabled {
		go a.maybeRefreshGEC()
	}
	return a.GetSettings()
}

// IngestStatus reports journal archive and watcher state.
type IngestStatus struct {
	Scanning           bool   `json:"scanning"`
	WatcherActive      bool   `json:"watcherActive"`
	WatcherError       string `json:"watcherError"`
	LastError          string `json:"lastError"`
	LastScanAt         string `json:"lastScanAt"`
	FilesTracked       int    `json:"filesTracked"`
	EventsStored       int    `json:"eventsStored"`
	FirstEventAt       string `json:"firstEventAt"`
	FirstEventGalactic string `json:"firstEventGalactic"`
	LastEventAt        string `json:"lastEventAt"`
	LastEventGalactic  string `json:"lastEventGalactic"`
	CurrentSystem      string `json:"currentSystem"`
	CurrentShip        string `json:"currentShip"`
}

// GetIngestStatus returns the journal ingestion status.
func (a *App) GetIngestStatus() (IngestStatus, error) {
	st := IngestStatus{Scanning: a.scanning.Load(), LastError: a.getErr()}
	if err := a.ready(); err != nil {
		return st, nil
	}
	st.WatcherActive, st.WatcherError = a.watcher.Active()
	st.LastScanAt, _ = db.GetSetting(a.db, settingLastScanAt)
	arc, err := journal.ArchiveStatus(a.db)
	if err != nil {
		return st, err
	}
	st.FilesTracked, st.EventsStored = arc.FilesTracked, arc.EventsStored
	st.FirstEventAt, st.LastEventAt = arc.FirstEventAt, arc.LastEventAt
	st.FirstEventGalactic = ed.ToGalactic(arc.FirstEventAt)
	st.LastEventGalactic = ed.ToGalactic(arc.LastEventAt)
	var sys sql.NullString
	_ = a.db.QueryRow(`SELECT system_name FROM journal_events WHERE event IN ('FSDJump','Location','CarrierJump') AND `+journal.CmdrFilterSQL+`
		ORDER BY timestamp DESC, id DESC LIMIT 1`).Scan(&sys)
	st.CurrentSystem = sys.String
	var raw, ev string
	if a.db.QueryRow(`SELECT event, raw_json FROM journal_events WHERE event IN ('LoadGame','ShipyardSwap') AND `+journal.CmdrFilterSQL+`
		ORDER BY timestamp DESC, id DESC LIMIT 1`).Scan(&ev, &raw) == nil {
		if ev == "LoadGame" {
			var lg journal.LoadGameEvent
			if json.Unmarshal([]byte(raw), &lg) == nil {
				st.CurrentShip = ed.ShipDisplayName(lg.Ship, lg.ShipLocalised)
				if lg.ShipName != "" {
					st.CurrentShip += " \u201c" + lg.ShipName + "\u201d"
				}
			}
		} else {
			var sw journal.ShipyardSwapEvent
			if json.Unmarshal([]byte(raw), &sw) == nil {
				st.CurrentShip = ed.ShipDisplayName(sw.ShipType, sw.ShipTypeLocalised)
			}
		}
	}
	return st, nil
}

// RescanJournals starts a full idempotent catch-up scan in the background.
func (a *App) RescanJournals() error {
	if err := a.ready(); err != nil {
		return err
	}
	go a.runScan()
	return nil
}

// RefreshGEC re-downloads the EDAstro Galactic Exploration Catalog.
func (a *App) RefreshGEC() (int, error) {
	if err := a.ready(); err != nil {
		return 0, err
	}
	n, err := a.gec.Refresh(a.ctx)
	if err != nil {
		return 0, err
	}
	a.populateMissingPOIs()
	a.changed("plan", 0)
	return n, nil
}

// OpenURL opens an http(s) URL in the user's default browser.
func (a *App) OpenURL(link string) error {
	u, err := url.Parse(link)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return fmt.Errorf("refusing to open %q", link)
	}
	runtime.BrowserOpenURL(a.ctx, link)
	return nil
}

// ToGalactic converts an ISO-8601 UTC timestamp to "33XX-MM-DD HH:mm:ss UTC".
func (a *App) ToGalactic(iso string) string { return ed.ToGalactic(iso) }

// FromGalactic converts "33XX-MM-DD HH:mm:ss UTC" to an ISO-8601 UTC timestamp.
func (a *App) FromGalactic(gst string) (string, error) { return ed.FromGalactic(gst) }

// ============================================================================
// Modes
// ============================================================================

// ModeWindowView is a mode window for display.
type ModeWindowView struct {
	ID            int64  `json:"id"`
	ModeID        int64  `json:"modeId"`
	ModeName      string `json:"modeName"`
	Color         string `json:"color"`
	Start         string `json:"start"`
	End           string `json:"end"`
	StartGalactic string `json:"startGalactic"`
	EndGalactic   string `json:"endGalactic"`
	Duration      string `json:"duration"`
}

// ModesState is everything the UI needs about modes.
type ModesState struct {
	Modes               []modes.Mode     `json:"modes"`
	ActiveModeID        int64            `json:"activeModeId"`
	ActiveSince         string           `json:"activeSince"`
	ActiveSinceGalactic string           `json:"activeSinceGalactic"`
	Windows             []ModeWindowView `json:"windows"`
}

// GetModesState returns modes, the active mode and all windows (newest first).
func (a *App) GetModesState() (ModesState, error) {
	if err := a.ready(); err != nil {
		return ModesState{}, err
	}
	var st ModesState
	ms, err := modes.List(a.db)
	if err != nil {
		return st, err
	}
	st.Modes = ms
	if st.Modes == nil {
		st.Modes = []modes.Mode{}
	}
	byID := map[int64]modes.Mode{}
	for _, m := range ms {
		byID[m.ID] = m
	}
	active, err := modes.Active(a.db)
	if err != nil {
		return st, err
	}
	if active != nil {
		st.ActiveModeID, st.ActiveSince, st.ActiveSinceGalactic = active.ModeID, active.Start, ed.ToGalactic(active.Start)
	} else {
		st.ActiveModeID = modes.DefaultFallbackModeID(a.db)
	}
	ws, err := modes.ListWindows(a.db)
	if err != nil {
		return st, err
	}
	st.Windows = []ModeWindowView{}
	for i := len(ws) - 1; i >= 0; i-- {
		w := ws[i]
		m := byID[w.ModeID]
		st.Windows = append(st.Windows, ModeWindowView{
			ID: w.ID, ModeID: w.ModeID, ModeName: m.Name, Color: m.Color, Start: w.Start, End: w.End,
			StartGalactic: ed.ToGalactic(w.Start), EndGalactic: ed.ToGalactic(w.End),
			Duration: expedition.Elapsed(w.Start, w.End),
		})
	}
	return st, nil
}

// CreateMode adds a custom mode.
func (a *App) CreateMode(name, description, color string) (modes.Mode, error) {
	if err := a.ready(); err != nil {
		return modes.Mode{}, err
	}
	m, err := modes.Create(a.db, name, description, color)
	if err == nil {
		a.changed("modes", 0)
	}
	return m, err
}

// UpdateMode edits a mode.
func (a *App) UpdateMode(id int64, name, description, color string) error {
	if err := a.ready(); err != nil {
		return err
	}
	err := modes.Update(a.db, id, name, description, color)
	if err == nil {
		a.changed("modes", 0)
	}
	return err
}

// DeleteMode deletes a custom mode and its windows.
func (a *App) DeleteMode(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	err := modes.Delete(a.db, id)
	if err == nil {
		a.changed("modes", 0)
	}
	return err
}

// SwitchMode makes modeID the active mode from now (0 = stop tracking a mode).
func (a *App) SwitchMode(modeID int64) (ModesState, error) {
	if err := a.ready(); err != nil {
		return ModesState{}, err
	}
	if err := db.WithTx(a.db, func(tx *sql.Tx) error { return modes.Switch(tx, modeID, "") }); err != nil {
		return ModesState{}, err
	}
	a.changed("modes", 0)
	return a.GetModesState()
}

// ModeWindowInput creates (ID 0) or edits a mode window. Start/End are
// ISO-8601 UTC; End "" means open-ended.
type ModeWindowInput struct {
	ID     int64  `json:"id"`
	ModeID int64  `json:"modeId"`
	Start  string `json:"start"`
	End    string `json:"end"`
}

// SaveModeWindow writes a window retroactively; overlapping windows are
// trimmed/split so that windows never overlap.
func (a *App) SaveModeWindow(in ModeWindowInput) (ModesState, error) {
	if err := a.ready(); err != nil {
		return ModesState{}, err
	}
	if err := db.WithTx(a.db, func(tx *sql.Tx) error {
		return modes.SetWindow(tx, in.ID, in.ModeID, in.Start, in.End)
	}); err != nil {
		return ModesState{}, err
	}
	a.changed("modes", 0)
	return a.GetModesState()
}

// DeleteModeWindow removes a window.
func (a *App) DeleteModeWindow(id int64) (ModesState, error) {
	if err := a.ready(); err != nil {
		return ModesState{}, err
	}
	if err := modes.DeleteWindow(a.db, id); err != nil {
		return ModesState{}, err
	}
	a.changed("modes", 0)
	return a.GetModesState()
}

// ============================================================================
// Expeditions & planner rows
// ============================================================================

// ListExpeditions returns all expeditions (newest first).
func (a *App) ListExpeditions() ([]expedition.Expedition, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	l, err := expedition.List(a.db)
	if l == nil {
		l = []expedition.Expedition{}
	}
	return l, err
}

// CreateExpedition adds an expedition and makes it the active one.
func (a *App) CreateExpedition(name, description string) (expedition.Expedition, error) {
	if err := a.ready(); err != nil {
		return expedition.Expedition{}, err
	}
	e, err := expedition.Create(a.db, name, description)
	if err != nil {
		return e, err
	}
	_ = db.SetSetting(a.db, settingActiveExp, strconv.FormatInt(e.ID, 10))
	a.changed("plan", e.ID)
	return e, nil
}

// CloneExpedition duplicates an expedition (and its waypoints/region headers)
// into a new expedition in the planning state, and makes the clone active.
func (a *App) CloneExpedition(id int64) (expedition.Expedition, error) {
	if err := a.ready(); err != nil {
		return expedition.Expedition{}, err
	}
	e, err := expedition.Clone(a.db, id)
	if err != nil {
		return e, err
	}
	_ = db.SetSetting(a.db, settingActiveExp, strconv.FormatInt(e.ID, 10))
	go a.kickResolution(e.ID, false)
	a.changed("plan", e.ID)
	return e, nil
}

// UpdateExpedition renames an expedition.
func (a *App) UpdateExpedition(id int64, name, description string) error {
	if err := a.ready(); err != nil {
		return err
	}
	err := expedition.UpdateMeta(a.db, id, name, description)
	if err == nil {
		a.changed("plan", id)
	}
	return err
}

// DeleteExpedition deletes an expedition plan (journal history is kept).
func (a *App) DeleteExpedition(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := expedition.Delete(a.db, id); err != nil {
		return err
	}
	if a.activeExpeditionID() == id {
		_ = db.SetSetting(a.db, settingActiveExp, "")
	}
	a.changed("plan", id)
	return nil
}

// SetActiveExpedition selects the expedition shown in the planner.
func (a *App) SetActiveExpedition(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := db.SetSetting(a.db, settingActiveExp, strconv.FormatInt(id, 10)); err != nil {
		return err
	}
	go a.kickResolution(id, false)
	return nil
}

// GetPlan returns the planner sheet for an expedition.
func (a *App) GetPlan(id int64) (expedition.PlanView, error) {
	if err := a.ready(); err != nil {
		return expedition.PlanView{}, err
	}
	return expedition.BuildPlan(a.db, id, a.gecLookup)
}

func (a *App) gecLookup(name string, addr int64) *expedition.GECRef {
	ps := a.gec.Lookup(name, addr)
	if len(ps) == 0 {
		return nil
	}
	return &expedition.GECRef{ID: ps[0].ID, Name: ps[0].Name, Type: ps[0].Type, URL: ps[0].URL, Count: len(ps)}
}

// AddRow inserts a row after afterRowID (0 = append, -1 = top). rowType is
// "waypoint" or "region"; regionID is used for region headers.
func (a *App) AddRow(expID, afterRowID int64, rowType string, regionID int) (expedition.Row, error) {
	if err := a.ready(); err != nil {
		return expedition.Row{}, err
	}
	var r expedition.Row
	err := db.WithTx(a.db, func(tx *sql.Tx) error {
		var err error
		r, err = expedition.InsertRow(tx, expID, afterRowID, rowType, regionID)
		return err
	})
	if err == nil {
		a.changed("plan", expID)
	}
	return r, err
}

// UpdateRow applies a partial update to a row. Changing the system name
// triggers asynchronous coordinate resolution and visit re-evaluation.
func (a *App) UpdateRow(patch expedition.RowPatch) error {
	if err := a.ready(); err != nil {
		return err
	}
	changed, err := expedition.UpdateRow(a.db, patch)
	if err != nil {
		return err
	}
	r, err := expedition.GetRow(a.db, patch.ID)
	if err != nil {
		return err
	}
	if changed {
		if r.SystemName != "" && strings.TrimSpace(r.POIName) == "" {
			if ps := a.gec.Lookup(r.SystemName, r.SystemAddress); len(ps) > 0 && strings.TrimSpace(ps[0].Name) != "" {
				poiName := html.UnescapeString(strings.TrimSpace(ps[0].Name))
				if _, err := a.db.Exec(`UPDATE expedition_rows SET poi_name = ? WHERE id = ? AND (poi_name IS NULL OR TRIM(poi_name) = '')`, poiName, r.ID); err == nil {
					r.POIName = poiName
				}
			}
		}
		a.reevaluate(r.ExpeditionID)
		if r.SystemName != "" {
			a.resolveRowAsync(r, true)
		}
	}
	a.changed("plan", r.ExpeditionID)
	return nil
}

// SetLegMode assigns a mode to the leg arriving at rowID and updates the
// corresponding time window in mode_windows when the waypoint has been visited.
func (a *App) SetLegMode(rowID, modeID int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	var expID int64
	err := db.WithTx(a.db, func(tx *sql.Tx) error {
		var err error
		expID, err = expedition.SetLegMode(tx, rowID, modeID)
		return err
	})
	if err != nil {
		return err
	}
	a.invalidateAllTimeCache()
	a.changed("modes", expID)
	return nil
}

// SetActiveLegMode sets the mode for the current in-progress leg (since the
// latest visited waypoint).
func (a *App) SetActiveLegMode(expID, modeID int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	err := db.WithTx(a.db, func(tx *sql.Tx) error {
		return expedition.SetActiveLegMode(tx, expID, modeID)
	})
	if err != nil {
		return err
	}
	a.invalidateAllTimeCache()
	a.changed("modes", expID)
	return nil
}

// RetryResolve re-runs coordinate resolution for a waypoint.
func (a *App) RetryResolve(rowID int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	r, err := expedition.GetRow(a.db, rowID)
	if err != nil {
		return err
	}
	if r.SystemName == "" {
		return nil
	}
	_, _ = a.db.Exec(`DELETE FROM system_cache WHERE name_key = ? AND source = 'notfound'`, strings.ToLower(r.SystemName))
	a.resolveRowAsync(r, true)
	return nil
}

// DeleteRow removes a row.
func (a *App) DeleteRow(rowID int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	var expID int64
	err := db.WithTx(a.db, func(tx *sql.Tx) error {
		var err error
		expID, err = expedition.DeleteRow(tx, rowID)
		return err
	})
	if err != nil {
		return err
	}
	a.reevaluate(expID)
	a.changed("plan", expID)
	return nil
}

// MoveRow moves a row to the zero-based index toIndex.
func (a *App) MoveRow(rowID int64, toIndex int) error {
	if err := a.ready(); err != nil {
		return err
	}
	r, _ := expedition.GetRow(a.db, rowID)
	var expID int64
	err := db.WithTx(a.db, func(tx *sql.Tx) error {
		var err error
		expID, err = expedition.MoveRow(tx, rowID, toIndex)
		return err
	})
	if err != nil {
		return err
	}
	if r.RowType == expedition.RowWaypoint {
		a.reevaluate(expID)
	}
	a.changed("plan", expID)
	return nil
}

// RegionOptions is the prioritised region dropdown content.
type RegionOptions struct {
	AnchorID          int               `json:"anchorId"`
	AnchorName        string            `json:"anchorName"`
	AnchorIsDefault   bool              `json:"anchorIsDefault"`
	SuggestedRegionID int               `json:"suggestedRegionId"` // region of the next waypoint's coordinates
	Options           []ed.RegionOption `json:"options"`
}

// GetRegionOptions returns regions ordered for a Region Header dropdown.
// rowID is either the header being edited (editing = true) or the row after
// which a new header will be inserted (0 = append at end, -1 = top).
func (a *App) GetRegionOptions(expID, rowID int64, editing bool) (RegionOptions, error) {
	if err := a.ready(); err != nil {
		return RegionOptions{}, err
	}
	rows, err := expedition.ListRows(a.db, expID)
	if err != nil {
		return RegionOptions{}, err
	}
	// cut = index of the first row *after* the header position.
	cut := len(rows)
	switch {
	case rowID == expedition.InsertAtTop:
		cut = 0
	case rowID > 0:
		for i, r := range rows {
			if r.ID == rowID {
				if editing {
					cut = i
				} else {
					cut = i + 1
				}
				break
			}
		}
	}
	anchor := 0
	for i := 0; i < cut && i < len(rows); i++ {
		if rows[i].RowType == expedition.RowRegion && rows[i].RegionID > 0 {
			anchor = rows[i].RegionID
		}
	}
	out := RegionOptions{AnchorIsDefault: anchor == 0}
	if anchor == 0 {
		anchor = ed.InnerOrionSpurID
	}
	reg, _ := ed.RegionByID(anchor)
	out.AnchorID, out.AnchorName = anchor, reg.Name
	out.Options = ed.PrioritizedRegions(anchor)
	start := cut
	if editing {
		start = cut + 1
	}
	for i := start; i < len(rows); i++ {
		if rows[i].IsWaypoint() && rows[i].HasCoords {
			if r, ok := ed.RegionAt(rows[i].X, rows[i].Y, rows[i].Z); ok {
				out.SuggestedRegionID = r.ID
			}
			break
		}
	}
	return out, nil
}

// StartCandidates lists recorded visits to Waypoint #1.
type StartCandidates struct {
	SystemName string               `json:"systemName"`
	Visits     []expedition.Arrival `json:"visits"`
}

// GetStartCandidates returns every recorded arrival at Waypoint #1 (newest first).
func (a *App) GetStartCandidates(expID int64) (StartCandidates, error) {
	if err := a.ready(); err != nil {
		return StartCandidates{}, err
	}
	sys, visits, err := expedition.StartCandidates(a.db, expID)
	if visits == nil {
		visits = []expedition.Arrival{}
	}
	return StartCandidates{SystemName: sys, Visits: visits}, err
}

func (a *App) lifecycle(expID int64, fn func(tx *sql.Tx) error) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := db.WithTx(a.db, fn); err != nil {
		return err
	}
	a.changed("plan", expID)
	return nil
}

// StartExpedition starts the expedition at the given visit timestamp
// (ISO-8601 UTC) and retroactively replays history from that moment.
func (a *App) StartExpedition(expID int64, timestamp string) error {
	return a.lifecycle(expID, func(tx *sql.Tx) error { return expedition.Start(tx, expID, timestamp) })
}

// StartExpeditionOnArrival activates the expedition and waits for the next
// arrival at Waypoint #1 to set the start timestamp.
func (a *App) StartExpeditionOnArrival(expID int64) error {
	return a.lifecycle(expID, func(tx *sql.Tx) error { return expedition.Arm(tx, expID) })
}

// CompleteExpedition ends the expedition now.
func (a *App) CompleteExpedition(expID int64) error {
	return a.lifecycle(expID, func(tx *sql.Tx) error { return expedition.Complete(tx, expID, "") })
}

// ReopenExpedition makes a completed expedition active again.
func (a *App) ReopenExpedition(expID int64) error {
	return a.lifecycle(expID, func(tx *sql.Tx) error { return expedition.Reopen(tx, expID) })
}

// GetWaypointDetails returns the expandable drawer content for a waypoint.
func (a *App) GetWaypointDetails(rowID int64) (expedition.WaypointDetails, error) {
	if err := a.ready(); err != nil {
		return expedition.WaypointDetails{}, err
	}
	return expedition.Details(a.db, rowID)
}

// ============================================================================
// Stats sidebar
// ============================================================================

// SidebarStats is the content of the Stats Sidebar.
type SidebarStats struct {
	ExpeditionID    int64         `json:"expeditionId"`
	Status          string        `json:"status"`
	Started         bool          `json:"started"`
	Start           string        `json:"start"`
	End             string        `json:"end"`
	StartGalactic   string        `json:"startGalactic"`
	EndGalactic     string        `json:"endGalactic"`
	EndIsNow            bool          `json:"endIsNow"`
	Duration            string        `json:"duration"`
	CareerStart         string        `json:"careerStart"`
	CareerStartGalactic string        `json:"careerStartGalactic"`
	CareerDuration      string        `json:"careerDuration"`
	FilterActive        bool          `json:"filterActive"`
	Live            stats.Metrics `json:"live"`        // from journals (mode-filtered)
	Adjustments     stats.Metrics `json:"adjustments"` // manual
	Expedition      stats.Metrics `json:"expedition"`  // live + adjustments
	AllJournals     stats.Metrics `json:"allJournals"` // all ingested journal events (mode-filtered)
	BaselineEnabled bool          `json:"baselineEnabled"`
	BaselineSource  string        `json:"baselineSource"`
	Baseline        stats.Metrics `json:"baseline"`
	Lifetime        stats.Metrics `json:"lifetime"` // baseline + expedition
}

// GetSidebarStats computes the sidebar for an expedition with mode filters.
func (a *App) GetSidebarStats(expID int64, filter stats.Filter) (SidebarStats, error) {
	if err := a.ready(); err != nil {
		return SidebarStats{}, err
	}
	e, err := expedition.Get(a.db, expID)
	if err != nil {
		return SidebarStats{}, err
	}
	s := SidebarStats{
		ExpeditionID: e.ID, Status: e.Status, Started: e.StartTimestamp != "",
		Start: e.StartTimestamp, End: e.EndTimestamp, FilterActive: filter.Enabled,
		Adjustments: e.Adjustments, BaselineEnabled: e.BaselineEnabled, BaselineSource: e.BaselineSource,
		Baseline: e.Baseline,
	}
	eng := stats.NewEngine(a.db)
	// Mode filters apply only to the current expedition, never to All Time.
	// All-time personal carrier data comes directly from Frontier's lifetime Statistics.
	if all, err := a.getAllTimeMetrics(); err == nil {
		s.AllJournals = all
	}

	// Career metrics across all ingested journals for active commanders (populated at all times).
	var firstEvent sql.NullString
	_ = a.db.QueryRow(`SELECT MIN(c.first_seen) FROM commanders c WHERE c.enabled = 1 AND c.first_seen IS NOT NULL AND c.first_seen != ''`).Scan(&firstEvent)
	if !firstEvent.Valid || firstEvent.String == "" {
		_ = a.db.QueryRow(`SELECT MIN(timestamp) FROM journal_events WHERE ` + journal.CmdrFilterSQL).Scan(&firstEvent)
	}
	if firstEvent.Valid && firstEvent.String != "" {
		s.CareerStart = firstEvent.String
		s.CareerStartGalactic = ed.ToGalactic(firstEvent.String)
		s.CareerDuration = expedition.Elapsed(firstEvent.String, "")
	}
	if s.Started {
		if s.End == "" {
			s.EndIsNow = true
			s.EndGalactic = ed.TimeToGalactic(timeNow())
		} else {
			s.EndGalactic = ed.ToGalactic(s.End)
		}
		s.StartGalactic = ed.ToGalactic(s.Start)
		s.Duration = expedition.Elapsed(s.Start, s.End)
		live, err := eng.Compute(stats.Range{After: e.StartTimestamp, Until: e.EndTimestamp}, &filter)
		if err != nil {
			return s, err
		}
		// Carrier operations metrics (carrier jumps, carrier distance, and tritium) for the expedition
		// come exclusively from the Spansh / manual route logger (e.Adjustments) to prevent duplication.
		// Non-operation personal navigation metrics (CarrierPassengerLy and total DistanceLy)
		// are preserved directly from journal events when the commander is aboard.
		live.CarrierJumps = 0
		live.CarrierDistanceLy = 0
		live.TritiumUsed = nil

		s.Live = live
		s.Expedition = live.Add(e.Adjustments)
	}
	return s, nil
}


