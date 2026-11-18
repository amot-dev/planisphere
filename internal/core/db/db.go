// Package db owns the embedded SQLite database (pure-Go modernc.org/sqlite).
package db

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver (CGO-free)
)

// Open opens (creating if needed) the database at path, applies the
// WAL / foreign key pragmas on every pooled connection, runs the schema and
// seeds default data.
func Open(path string) (*sql.DB, error) {
	if path != ":memory:" {
		if mkdirError := os.MkdirAll(filepath.Dir(path), 0o755); mkdirError != nil {
			return nil, fmt.Errorf("create db dir: %w", mkdirError)
		}
	}
	queryValues := url.Values{}
	queryValues.Add("_pragma", "journal_mode(WAL)")
	queryValues.Add("_pragma", "foreign_keys(1)")
	queryValues.Add("_pragma", "busy_timeout(10000)")
	queryValues.Add("_pragma", "synchronous(NORMAL)")
	// Take the write lock at BEGIN so concurrent writers (live tailer and UI)
	// wait on busy_timeout instead of failing with SQLITE_BUSY on upgrade.
	queryValues.Add("_txlock", "immediate")
	dsn := "file:" + path + "?" + queryValues.Encode()
	if path == ":memory:" {
		// A shared in-memory DB so every pooled connection sees the same data (tests).
		dsn = "file:memdb" + fmt.Sprint(time.Now().UnixNano()) + "?mode=memory&cache=shared&" + queryValues.Encode()
	}
	dbConn, openError := sql.Open("sqlite", dsn)
	if openError != nil {
		return nil, openError
	}
	if path == ":memory:" {
		dbConn.SetMaxOpenConns(1)
	} else {
		// Limit open connections to serialize writers in Go memory and avoid SQLite file-lock contention.
		dbConn.SetMaxOpenConns(4)
		dbConn.SetMaxIdleConns(4)
	}
	if _, pragmaError := dbConn.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; PRAGMA synchronous=NORMAL; PRAGMA cache_size=-64000; PRAGMA temp_store=MEMORY;`); pragmaError != nil {
		dbConn.Close()
		return nil, fmt.Errorf("pragmas: %w", pragmaError)
	}
	if migrateError := Migrate(dbConn); migrateError != nil {
		dbConn.Close()
		return nil, migrateError
	}
	return dbConn, nil
}

// Queryer is satisfied by *sql.DB and *sql.Tx.
type Queryer interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
	Prepare(query string) (*sql.Stmt, error)
}

func getMetaSetting(queryer Queryer, key string) (string, error) {
	var settingValue string
	queryError := queryer.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&settingValue)
	if errors.Is(queryError, sql.ErrNoRows) {
		return "", nil
	}
	return settingValue, queryError
}

func setMetaSetting(queryer Queryer, key, value string) error {
	_, execError := queryer.Exec(`INSERT INTO settings(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return execError
}

// WithTx runs txFunc inside a transaction.
func WithTx(dbConn *sql.DB, txFunc func(transaction *sql.Tx) error) (returnedError error) {
	transaction, beginError := dbConn.Begin()
	if beginError != nil {
		return beginError
	}
	defer func() {
		if panicValue := recover(); panicValue != nil {
			_ = transaction.Rollback()
			panic(panicValue)
		} else if returnedError != nil {
			_ = transaction.Rollback()
		}
	}()

	if txError := txFunc(transaction); txError != nil {
		return txError
	}
	return transaction.Commit()
}
