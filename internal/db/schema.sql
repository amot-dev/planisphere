-- Elite Dangerous Expedition Planner — embedded SQLite schema.
--
-- "Ingest-Once, Persist-Forever": every relevant journal event is copied into
-- journal_events (including its raw JSON) the first time it is seen, so the
-- original Journal.*.log files can be deleted without losing any history.
--
-- All timestamps are stored as journal-style ISO-8601 UTC strings
-- ("2026-01-18T19:42:10Z"), which sort lexicographically in time order.

CREATE TABLE IF NOT EXISTS settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Commanders observed across ingested journals. enabled = 1 by default so all
-- commanders are included unless toggled off in Settings.
CREATE TABLE IF NOT EXISTS commanders (
    name       TEXT PRIMARY KEY COLLATE NOCASE,
    fid        TEXT NOT NULL DEFAULT '',
    enabled    INTEGER NOT NULL DEFAULT 1,
    first_seen TEXT,
    last_seen  TEXT
);

-- One row per journal file seen; byte_offset is the position after the last
-- complete line ingested, so tailing resumes exactly where it left off.
CREATE TABLE IF NOT EXISTS ingested_journals (
    file_name       TEXT PRIMARY KEY,
    file_path       TEXT NOT NULL,
    byte_offset     INTEGER NOT NULL DEFAULT 0,
    line_count      INTEGER NOT NULL DEFAULT 0,
    file_size       INTEGER NOT NULL DEFAULT 0,
    events_stored   INTEGER NOT NULL DEFAULT 0,
    first_timestamp TEXT,
    last_timestamp  TEXT,
    updated_at      TEXT NOT NULL
);

-- Deduplicated journal events. event_uid = SHA256(timestamp + event + key identifiers).
CREATE TABLE IF NOT EXISTS journal_events (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    event_uid      TEXT NOT NULL UNIQUE,
    timestamp      TEXT NOT NULL,
    event          TEXT NOT NULL,
    system_name    TEXT,
    system_address INTEGER,
    body_id        INTEGER,
    body_name      TEXT,
    star_x         REAL,
    star_y         REAL,
    star_z         REAL,
    commander      TEXT,
    source_file    TEXT,
    raw_json       TEXT NOT NULL,
    ingested_at    TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_journal_events_ts       ON journal_events(timestamp);
CREATE INDEX IF NOT EXISTS idx_journal_events_event_ts ON journal_events(event, timestamp);
CREATE INDEX IF NOT EXISTS idx_journal_events_system   ON journal_events(system_name COLLATE NOCASE);
CREATE INDEX IF NOT EXISTS idx_journal_events_body     ON journal_events(system_address, body_id);
CREATE INDEX IF NOT EXISTS idx_journal_events_cmdr     ON journal_events(commander COLLATE NOCASE);
CREATE INDEX IF NOT EXISTS idx_journal_events_source   ON journal_events(source_file);

-- Tracking modes (Exploration / Tourism / Travel + user-defined).
CREATE TABLE IF NOT EXISTS modes (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL UNIQUE COLLATE NOCASE,
    description TEXT NOT NULL DEFAULT '',
    color       TEXT NOT NULL DEFAULT '#FF7100',
    is_default  INTEGER NOT NULL DEFAULT 0,
    sort_order  INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL
);

-- Time windows during which a mode was active. end_timestamp NULL = still active.
-- Windows never overlap (enforced by the application when windows are written).
CREATE TABLE IF NOT EXISTS mode_windows (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    mode_id         INTEGER NOT NULL REFERENCES modes(id) ON DELETE CASCADE,
    start_timestamp TEXT NOT NULL,
    end_timestamp   TEXT,
    CHECK (end_timestamp IS NULL OR end_timestamp > start_timestamp)
);
CREATE INDEX IF NOT EXISTS idx_mode_windows_start ON mode_windows(start_timestamp);

CREATE TABLE IF NOT EXISTS expeditions (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    name             TEXT NOT NULL,
    description      TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'planning' CHECK (status IN ('planning', 'active', 'completed')),
    start_timestamp  TEXT,
    end_timestamp    TEXT,
    armed_at         TEXT,   -- "wait for first arrival at Waypoint #1" requested at this time
    baseline_enabled INTEGER NOT NULL DEFAULT 0,
    baseline_source  TEXT NOT NULL DEFAULT '',   -- '', 'statistics', 'manual'
    baseline_json    TEXT NOT NULL DEFAULT '{}', -- stats.Metrics snapshot "before expedition"
    adjustments_json TEXT NOT NULL DEFAULT '{}', -- stats.Metrics manual deltas (e.g. remote carrier jumps)
    carrier_type          TEXT NOT NULL DEFAULT 'player',
    carrier_weekly_upkeep INTEGER NOT NULL DEFAULT 0,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL
);

-- Planner rows: waypoints and galactic region headers, ordered by position.
CREATE TABLE IF NOT EXISTS expedition_rows (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    expedition_id   INTEGER NOT NULL REFERENCES expeditions(id) ON DELETE CASCADE,
    position        INTEGER NOT NULL,
    row_type        TEXT NOT NULL CHECK (row_type IN ('waypoint', 'region')),
    region_id       INTEGER,
    fc_stop         INTEGER NOT NULL DEFAULT 0,
    fc_arrived      INTEGER NOT NULL DEFAULT 0,
    leg_mode_id     INTEGER NOT NULL DEFAULT 0,
    system_name     TEXT NOT NULL DEFAULT '',
    poi_name        TEXT NOT NULL DEFAULT '',
    notes           TEXT NOT NULL DEFAULT '',
    system_address  INTEGER,
    edsm_id         INTEGER,
    x               REAL,
    y               REAL,
    z               REAL,
    coords_source   TEXT NOT NULL DEFAULT '', -- '', 'journal', 'edsm', 'notfound', 'error'
    visited_at      TEXT,
    visit_event_uid TEXT,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_expedition_rows_exp ON expedition_rows(expedition_id, position);

-- Resolved system coordinates (journal or EDSM), shared across expeditions.
CREATE TABLE IF NOT EXISTS system_cache (
    name_key       TEXT PRIMARY KEY, -- lower-cased system name
    system_name    TEXT NOT NULL,
    system_address INTEGER,
    edsm_id        INTEGER,
    x              REAL,
    y              REAL,
    z              REAL,
    source         TEXT NOT NULL,
    fetched_at     TEXT NOT NULL
);

-- EDAstro Galactic Exploration Catalog POIs.
CREATE TABLE IF NOT EXISTS gec_pois_cache (
    id             INTEGER PRIMARY KEY,
    name           TEXT NOT NULL,
    system_name    TEXT NOT NULL,
    system_address INTEGER,
    poi_type       TEXT NOT NULL DEFAULT '',
    region         TEXT NOT NULL DEFAULT '',
    summary        TEXT NOT NULL DEFAULT '',
    poi_url        TEXT NOT NULL DEFAULT '',
    x              REAL,
    y              REAL,
    z              REAL,
    raw_json       TEXT NOT NULL,
    source         TEXT NOT NULL DEFAULT 'GEC',
    fetched_at     TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_gec_system_name ON gec_pois_cache(system_name COLLATE NOCASE);
CREATE INDEX IF NOT EXISTS idx_gec_system_addr ON gec_pois_cache(system_address);

