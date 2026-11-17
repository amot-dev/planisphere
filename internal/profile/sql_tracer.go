package profile

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"planisphere/internal/core/db"
)

type rawSQLEvent struct {
	programCounters [16]uintptr
	counterCount    int
	subSpanLabel    string
	query           string
	totalDuration   time.Duration
	sqlDuration     time.Duration
	rowsScanned     int
}

type stackFrame struct {
	functionName string
	location     string
}

var (
	registerDriverOnce sync.Once
	activeRecorderMu   sync.RWMutex
	globalRecorder     *treeRecorder
)

func setActiveRecorder(recorder *treeRecorder) {
	activeRecorderMu.Lock()
	globalRecorder = recorder
	activeRecorderMu.Unlock()
}

func currentRecorder() *treeRecorder {
	activeRecorderMu.RLock()
	recorder := globalRecorder
	activeRecorderMu.RUnlock()
	if recorder != nil && recorder.isRecording() {
		return recorder
	}
	return nil
}

// openTracedDatabase initializes the snapshot database with standard migrations
// and reopens it through a profiling driver wrapper that attributes SQL queries
// and row-scan loops to internal call stacks.
func openTracedDatabase(snapshotPath string) (*sql.DB, error) {
	bootstrapConn, openError := db.Open(snapshotPath)
	if openError != nil {
		return nil, openError
	}
	underlyingDriver := bootstrapConn.Driver()
	_ = bootstrapConn.Close()

	registerDriverOnce.Do(func() {
		sql.Register("sqlite_profile_traced", &tracedDriver{inner: underlyingDriver})
	})

	queryValues := url.Values{}
	queryValues.Add("_pragma", "journal_mode(WAL)")
	queryValues.Add("_pragma", "foreign_keys(1)")
	queryValues.Add("_pragma", "busy_timeout(10000)")
	queryValues.Add("_pragma", "synchronous(NORMAL)")
	queryValues.Add("_txlock", "immediate")
	dsn := "file:" + snapshotPath + "?" + queryValues.Encode()

	tracedConn, tracedOpenError := sql.Open("sqlite_profile_traced", dsn)
	if tracedOpenError != nil {
		return nil, tracedOpenError
	}
	tracedConn.SetMaxOpenConns(4)
	tracedConn.SetMaxIdleConns(4)
	if _, pragmaError := tracedConn.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; PRAGMA synchronous=NORMAL; PRAGMA cache_size=-64000; PRAGMA temp_store=MEMORY;`); pragmaError != nil {
		_ = tracedConn.Close()
		return nil, fmt.Errorf("apply traced pragmas: %w", pragmaError)
	}
	return tracedConn, nil
}

type tracedDriver struct {
	inner driver.Driver
}

func ( drv *tracedDriver) Open(name string) (driver.Conn, error) {
	innerConn, openError := drv.inner.Open(name)
	if openError != nil {
		return nil, openError
	}
	return &tracedConn{inner: innerConn}, nil
}

type tracedConn struct {
	inner driver.Conn
}

func (conn *tracedConn) Prepare(query string) (driver.Stmt, error) {
	stmt, prepError := conn.inner.Prepare(query)
	if prepError != nil {
		return nil, prepError
	}
	return &tracedStmt{inner: stmt, query: query}, nil
}

func (conn *tracedConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if prepCtx, ok := conn.inner.(driver.ConnPrepareContext); ok {
		stmt, prepError := prepCtx.PrepareContext(ctx, query)
		if prepError != nil {
			return nil, prepError
		}
		return &tracedStmt{inner: stmt, query: query}, nil
	}
	return conn.Prepare(query)
}

func (conn *tracedConn) Close() error { return conn.inner.Close() }

func (conn *tracedConn) Begin() (driver.Tx, error) { return conn.inner.Begin() }

func (conn *tracedConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if beginTx, ok := conn.inner.(driver.ConnBeginTx); ok {
		return beginTx.BeginTx(ctx, opts)
	}
	return conn.inner.Begin()
}

func (conn *tracedConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	execer, ok := conn.inner.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	recorder := currentRecorder()
	if recorder == nil {
		return execer.ExecContext(ctx, query, args)
	}
	startTime := time.Now()
	result, execError := execer.ExecContext(ctx, query, args)
	elapsed := time.Since(startTime)
	var counters [16]uintptr
	count := runtime.Callers(3, counters[:])
	recorder.recordSQLEvent(rawSQLEvent{
		programCounters: counters,
		counterCount:    count,
		query:           query,
		totalDuration:   elapsed,
		sqlDuration:     elapsed,
	})
	return result, execError
}

func (conn *tracedConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	queryer, ok := conn.inner.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	recorder := currentRecorder()
	if recorder == nil {
		return queryer.QueryContext(ctx, query, args)
	}
	startTime := time.Now()
	rows, queryError := queryer.QueryContext(ctx, query, args)
	sqlInitDuration := time.Since(startTime)
	if queryError != nil {
		return nil, queryError
	}
	var counters [16]uintptr
	count := runtime.Callers(3, counters[:])
	return &tracedRows{
		inner:           rows,
		recorder:        recorder,
		query:           query,
		programCounters: counters,
		counterCount:    count,
		subSpanLabel:    recorder.currentSubSpan(),
		queryStartTime:  startTime,
		sqlDuration:     sqlInitDuration,
	}, nil
}

type tracedStmt struct {
	inner driver.Stmt
	query string
}

func (stmt *tracedStmt) Close() error  { return stmt.inner.Close() }
func (stmt *tracedStmt) NumInput() int { return stmt.inner.NumInput() }

func (stmt *tracedStmt) Exec(args []driver.Value) (driver.Result, error) {
	recorder := currentRecorder()
	if recorder == nil {
		return stmt.inner.Exec(args)
	}
	startTime := time.Now()
	result, execError := stmt.inner.Exec(args)
	elapsed := time.Since(startTime)
	var counters [16]uintptr
	count := runtime.Callers(3, counters[:])
	recorder.recordSQLEvent(rawSQLEvent{
		programCounters: counters,
		counterCount:    count,
		query:           stmt.query,
		totalDuration:   elapsed,
		sqlDuration:     elapsed,
	})
	return result, execError
}

func (stmt *tracedStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	stmtExecer, ok := stmt.inner.(driver.StmtExecContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	recorder := currentRecorder()
	if recorder == nil {
		return stmtExecer.ExecContext(ctx, args)
	}
	startTime := time.Now()
	result, execError := stmtExecer.ExecContext(ctx, args)
	elapsed := time.Since(startTime)
	var counters [16]uintptr
	count := runtime.Callers(3, counters[:])
	recorder.recordSQLEvent(rawSQLEvent{
		programCounters: counters,
		counterCount:    count,
		query:           stmt.query,
		totalDuration:   elapsed,
		sqlDuration:     elapsed,
	})
	return result, execError
}

func (stmt *tracedStmt) Query(args []driver.Value) (driver.Rows, error) {
	recorder := currentRecorder()
	if recorder == nil {
		return stmt.inner.Query(args)
	}
	startTime := time.Now()
	rows, queryError := stmt.inner.Query(args)
	sqlInitDuration := time.Since(startTime)
	if queryError != nil {
		return nil, queryError
	}
	var counters [16]uintptr
	count := runtime.Callers(3, counters[:])
	return &tracedRows{
		inner:           rows,
		recorder:        recorder,
		query:           stmt.query,
		programCounters: counters,
		counterCount:    count,
		subSpanLabel:    recorder.currentSubSpan(),
		queryStartTime:  startTime,
		sqlDuration:     sqlInitDuration,
	}, nil
}

func (stmt *tracedStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	stmtQueryer, ok := stmt.inner.(driver.StmtQueryContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	recorder := currentRecorder()
	if recorder == nil {
		return stmtQueryer.QueryContext(ctx, args)
	}
	startTime := time.Now()
	rows, queryError := stmtQueryer.QueryContext(ctx, args)
	sqlInitDuration := time.Since(startTime)
	if queryError != nil {
		return nil, queryError
	}
	var counters [16]uintptr
	count := runtime.Callers(3, counters[:])
	return &tracedRows{
		inner:           rows,
		recorder:        recorder,
		query:           stmt.query,
		programCounters: counters,
		counterCount:    count,
		subSpanLabel:    recorder.currentSubSpan(),
		queryStartTime:  startTime,
		sqlDuration:     sqlInitDuration,
	}, nil
}

type tracedRows struct {
	inner           driver.Rows
	recorder        *treeRecorder
	query           string
	programCounters [16]uintptr
	counterCount    int
	subSpanLabel    string
	queryStartTime  time.Time
	sqlDuration     time.Duration
	rowsScanned     int
	closed          bool
}

func (rows *tracedRows) Columns() []string { return rows.inner.Columns() }

func (rows *tracedRows) Next(dest []driver.Value) error {
	stepStart := time.Now()
	stepError := rows.inner.Next(dest)
	rows.sqlDuration += time.Since(stepStart)
	if stepError == nil {
		rows.rowsScanned++
	}
	return stepError
}

func (rows *tracedRows) Close() error {
	closeStart := time.Now()
	closeError := rows.inner.Close()
	closeEnd := time.Now()
	rows.sqlDuration += closeEnd.Sub(closeStart)
	if !rows.closed {
		rows.closed = true
		rows.recorder.recordSQLEvent(rawSQLEvent{
			programCounters: rows.programCounters,
			counterCount:    rows.counterCount,
			subSpanLabel:    rows.subSpanLabel,
			query:           rows.query,
			totalDuration:   closeEnd.Sub(rows.queryStartTime),
			sqlDuration:     rows.sqlDuration,
			rowsScanned:     rows.rowsScanned,
		})
	}
	return closeError
}

func resolveInternalStackFrames(programCounters []uintptr) []stackFrame {
	if len(programCounters) == 0 {
		return nil
	}
	framesIterator := runtime.CallersFrames(programCounters)
	var internalFrames []stackFrame
	for {
		frame, hasMore := framesIterator.Next()
		if strings.Contains(frame.Function, "planisphere/internal/") &&
			!strings.Contains(frame.Function, "planisphere/internal/profile") &&
			!strings.Contains(frame.Function, "planisphere/internal/core/db.WithTx") {
			shortFunc := strings.TrimPrefix(frame.Function, "planisphere/internal/")
			shortFile := extractInternalRelativePath(frame.File)
			location := fmt.Sprintf("%s:%d", shortFile, frame.Line)
			if len(internalFrames) == 0 || internalFrames[len(internalFrames)-1].functionName != shortFunc {
				internalFrames = append(internalFrames, stackFrame{
					functionName: shortFunc,
					location:     location,
				})
			}
		}
		if !hasMore {
			break
		}
	}
	for left, right := 0, len(internalFrames)-1; left < right; left, right = left+1, right-1 {
		internalFrames[left], internalFrames[right] = internalFrames[right], internalFrames[left]
	}
	return internalFrames
}

func extractInternalRelativePath(fullPath string) string {
	normalized := filepath.ToSlash(fullPath)
	if idx := strings.Index(normalized, "internal/"); idx >= 0 {
		return normalized[idx:]
	}
	return filepath.Base(normalized)
}

func explainQueryPlan(database *sql.DB, query string) (planText string, hasFullScan bool, hasTempBTree bool) {
	trimmed := strings.TrimSpace(query)
	upperPrefix := strings.ToUpper(trimmed)
	if !strings.HasPrefix(upperPrefix, "SELECT") && !strings.HasPrefix(upperPrefix, "UPDATE") && !strings.HasPrefix(upperPrefix, "DELETE") {
		return "", false, false
	}
	rows, explainError := database.Query("EXPLAIN QUERY PLAN " + trimmed)
	if explainError != nil {
		return "", false, false
	}
	defer rows.Close()

	var details []string
	for rows.Next() {
		var nodeID, parentID, notUsed int
		var detail string
		if rows.Scan(&nodeID, &parentID, &notUsed, &detail) == nil {
			details = append(details, detail)
			upperDetail := strings.ToUpper(detail)
			if isUnindexedDataTableScan(upperDetail) {
				hasFullScan = true
			}
			if strings.Contains(upperDetail, "TEMP B-TREE") {
				hasTempBTree = true
			}
		}
	}
	return strings.Join(details, " | "), hasFullScan, hasTempBTree
}

func isUnindexedDataTableScan(upperDetail string) bool {
	if !strings.HasPrefix(upperDetail, "SCAN ") {
		return false
	}
	if strings.Contains(upperDetail, "USING INDEX") || strings.Contains(upperDetail, "USING COVERING INDEX") {
		return false
	}
	for _, table := range []string{"JOURNAL_EVENTS", "EXPEDITION_ROWS", "GEC_POIS_CACHE", "SYSTEM_CACHE", "INGESTED_JOURNALS"} {
		if strings.Contains(upperDetail, table) {
			return true
		}
	}
	return false
}