package ingest

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/db"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/pricing"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/tokenizer"
	_ "modernc.org/sqlite"
)

const (
	SQLiteQuietPeriod  = 150 * time.Millisecond
	SQLiteForceRescan  = 30 * time.Second
	SQLiteWatchBatchMs = 250 * time.Millisecond
)

type sqliteCursor struct {
	At    int64  `json:"at"`
	ID    string `json:"id"`
	Inode string `json:"inode"`
}

type sqliteSigCache struct {
	key      string
	caughtUp bool
	at       time.Time
}

// SQLiteReader passively reads an external agent-owned SQLite DB in read-only mode.
type SQLiteReader struct {
	store    *db.Store
	path     string
	spec     AgentSpec
	counter  *tokenizer.Counter
	onChange func()

	mu          sync.Mutex
	status      CollectorStatus
	sigCache    *sqliteSigCache
	draining    bool
	quietMode   bool
	scanning    bool
	scanPending bool

	watcher    *fsnotify.Watcher
	stopCh     chan struct{}
	debounceMu sync.Mutex
	debounce   *time.Timer
	retryTimer *time.Timer
}

// NewSQLiteReader constructs a passive SQLite reader for spec and database path.
func NewSQLiteReader(store *db.Store, path string, spec AgentSpec, onChange func()) *SQLiteReader {
	return NewSQLiteReaderWithCounter(store, path, spec, tokenizer.NewCounter(""), onChange)
}

// NewSQLiteReaderWithCounter constructs a passive SQLite reader with a shared hidden tokenizer Counter.
func NewSQLiteReaderWithCounter(store *db.Store, path string, spec AgentSpec, counter *tokenizer.Counter, onChange func()) *SQLiteReader {
	if counter == nil {
		counter = tokenizer.NewCounter("")
	}
	if onChange == nil {
		onChange = func() {}
	}
	return &SQLiteReader{
		store:    store,
		path:     path,
		spec:     spec,
		counter:  counter,
		onChange: onChange,
		status:   CollectorStatus{State: "starting"},
		stopCh:   make(chan struct{}),
	}
}

// Status returns a snapshot of the SQLite reader status.
func (r *SQLiteReader) Status() CollectorStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status
}

// Start enables live quiet-period mode, fsnotify watchers, and 60s safety scans.
func (r *SQLiteReader) Start() {
	r.mu.Lock()
	r.quietMode = true
	r.mu.Unlock()

	r.setupWatcher()
	r.Scan()

	go func() {
		ticker := time.NewTicker(RescanInterval)
		defer ticker.Stop()
		for {
			select {
			case <-r.stopCh:
				return
			case <-ticker.C:
				r.Scan()
			}
		}
	}()
}

// Stop halts watchers and timers.
func (r *SQLiteReader) Stop() {
	select {
	case <-r.stopCh:
		return
	default:
		close(r.stopCh)
	}
	r.debounceMu.Lock()
	if r.debounce != nil {
		r.debounce.Stop()
	}
	if r.retryTimer != nil {
		r.retryTimer.Stop()
	}
	r.debounceMu.Unlock()
	if r.watcher != nil {
		_ = r.watcher.Close()
	}
}

func (r *SQLiteReader) setupWatcher() {
	if r.path == "" {
		return
	}
	dir := filepath.Dir(r.path)
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
		return
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return
	}
	if err := w.Add(dir); err != nil {
		_ = w.Close()
		return
	}
	r.watcher = w
	base := filepath.Base(r.path)
	wal := base + "-wal"
	go func() {
		for {
			select {
			case <-r.stopCh:
				return
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				name := filepath.Base(ev.Name)
				if IsAuxiliaryFile(name) {
					continue
				}
				if name == base || name == wal {
					r.schedule(SQLiteWatchBatchMs)
				}
			case _, ok := <-w.Errors:
				if !ok {
					return
				}
			}
		}
	}()
}

func (r *SQLiteReader) schedule(d time.Duration) {
	r.debounceMu.Lock()
	defer r.debounceMu.Unlock()
	if r.debounce != nil {
		r.debounce.Stop()
	}
	r.debounce = time.AfterFunc(d, func() {
		select {
		case <-r.stopCh:
			return
		default:
			r.Scan()
		}
	})
}

func (r *SQLiteReader) scheduleRetry() {
	r.debounceMu.Lock()
	defer r.debounceMu.Unlock()
	if r.retryTimer != nil {
		return
	}
	r.retryTimer = time.AfterFunc(SQLiteQuietPeriod+15*time.Millisecond, func() {
		r.debounceMu.Lock()
		r.retryTimer = nil
		r.debounceMu.Unlock()
		select {
		case <-r.stopCh:
			return
		default:
			r.Scan()
		}
	})
}

// Scan executes a single-flight passive SQLite read if admitted.
func (r *SQLiteReader) Scan() bool {
	r.mu.Lock()
	if r.scanning {
		r.scanPending = true
		r.mu.Unlock()
		return false
	}
	r.scanning = true
	r.mu.Unlock()

	changed := r.scanBody()

	r.mu.Lock()
	r.scanning = false
	rescan := r.scanPending
	r.scanPending = false
	r.mu.Unlock()

	if rescan {
		r.schedule(100 * time.Millisecond)
	}
	return changed
}

func (r *SQLiteReader) scanBody() bool {
	now := r.store.NowFn()
	nowISO := now.UTC().Format("2006-01-02T15:04:05.000Z")
	if r.path == "" {
		r.mu.Lock()
		r.status = CollectorStatus{State: "waiting", Reason: "Sin base de datos", LastScan: nowISO}
		r.mu.Unlock()
		return false
	}
	if !IsRegularFile(r.path) {
		r.mu.Lock()
		r.status = CollectorStatus{State: "waiting", Reason: "Sin base de datos", LastScan: nowISO}
		r.mu.Unlock()
		return false
	}

	sig, err := SQLiteSignature(r.path)
	if err != nil {
		return false
	}

	r.mu.Lock()
	prev := r.sigCache
	quiet := r.quietMode
	draining := r.draining
	r.mu.Unlock()

	if prev != nil && prev.key == sig.Key && prev.caughtUp && !draining && now.Sub(prev.at) < SQLiteForceRescan {
		r.mu.Lock()
		r.status.State = "running"
		r.status.Version = r.spec.ExpectedVersion
		r.status.RecordsRead = 0
		r.mu.Unlock()
		return false
	}
	if quiet && now.UnixMilli()-sig.LatestMs < SQLiteQuietPeriod.Milliseconds() {
		r.scheduleRetry()
		return false
	}

	absPath, err := filepath.Abs(r.path)
	if err != nil {
		absPath = r.path
	}
	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(50)", strings.ReplaceAll(url.PathEscape(absPath), "%2F", "/"))

	srcDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		r.mu.Lock()
		r.status = CollectorStatus{State: "error", LastScan: nowISO}
		r.mu.Unlock()
		r.store.RecordError(r.spec.ID+"_scan_failed", r.path)
		return false
	}
	defer srcDB.Close()

	var curRaw string
	cur := sqliteCursor{}
	if r.store.SQL.QueryRow(`SELECT data FROM cursors WHERE path=?`, r.path).Scan(&curRaw) == nil && curRaw != "" {
		_ = json.Unmarshal([]byte(curRaw), &cur)
	}
	inodeStr := strconv.FormatUint(sig.Inode, 10)
	if cur.Inode != inodeStr {
		cur.At = 0
		cur.ID = ""
		cur.Inode = inodeStr
	}

	rows, err := srcDB.Query(r.spec.QuerySQL, cur.At, cur.At, cur.ID)
	if err != nil {
		r.mu.Lock()
		r.status = CollectorStatus{State: "error", LastScan: nowISO}
		r.mu.Unlock()
		r.store.RecordError(r.spec.ID+"_scan_failed", r.path)
		return false
	}
	defer rows.Close()

	colNames, err := rows.Columns()
	if err != nil {
		return false
	}

	activationMs := r.store.ActivationTime().UnixMilli()
	payloadCol := r.spec.Columns["payload"]
	if payloadCol == "" {
		payloadCol = "data"
	}
	idCol := r.spec.Columns["id"]
	sessCol := r.spec.Columns["sessionId"]
	updCol := r.spec.Columns["updated"]
	verCol := r.spec.Columns["version"]
	typeCol := r.spec.Columns["type"]

	rowCount := 0
	changed := false
	var lastAt int64
	var lastID string

	for rows.Next() {
		rowCount++
		vals := make([]any, len(colNames))
		ptrs := make([]any, len(colNames))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			continue
		}
		rowMap := make(map[string]any, len(colNames))
		for i, col := range colNames {
			v := vals[i]
			if b, ok := v.([]byte); ok {
				rowMap[col] = string(b)
			} else {
				rowMap[col] = v
			}
		}

		rowID := CanonicalID(rowMap[idCol])
		if rowID == "" {
			rowID = CanonicalID(rowMap["id"])
		}
		if updCol != "" {
			if ts, err := strconv.ParseInt(CanonicalID(rowMap[updCol]), 10, 64); err == nil {
				lastAt = ts
			}
		}
		lastID = rowID

		payloadStr := CanonicalID(rowMap[payloadCol])
		if len(payloadStr) > MaxChunkBytes {
			r.store.RecordError(r.spec.ID+"_record_limit", rowID)
			continue
		}
		rec, err := ParseJSONPreservingNumbers([]byte(payloadStr))
		if err != nil {
			r.store.RecordError(r.spec.ID+"_invalid_usage", rowID)
			continue
		}
		if idCol != "" && rowID != "" && rec["id"] != nil && CanonicalID(rec["id"]) != rowID {
			r.store.RecordError(r.spec.ID+"_id_mismatch", rowID)
			continue
		}
		if rowID != "" {
			rec["id"] = rowID
		}
		if typeCol != "" && CanonicalID(rowMap[typeCol]) != "" {
			rec["type"] = CanonicalID(rowMap[typeCol])
		}

		ctx := map[string]string{
			"sessionId": CanonicalID(rowMap[sessCol]),
			"version":   CanonicalID(rowMap[verCol]),
		}
		rawEv, err := CatalogMessage(r.spec, rec, ctx)
		if err != nil {
			r.classifySQLiteError(err, rowID)
			continue
		}
		if rawEv == nil {
			// Silent pure-Go hidden tokenizer fallback when local record has visible text without native counters.
			if fb, ok := ExtractFallbackMeta(r.spec, rec, ctx); ok {
				if fb.OccurredAt.UnixMilli() >= activationMs {
					res := r.counter.Count(fb.Provider, fb.Model, fb.Text)
					if err := r.store.SaveTextMeasurement(db.TextMeasurement{
						Source:     fb.Source,
						Provider:   fb.Provider,
						Agent:      fb.Agent,
						Model:      fb.Model,
						SessionID:  fb.SessionID,
						ResponseID: fb.ResponseID,
						OccurredAt: fb.OccurredAt.Format("2006-01-02T15:04:05.000Z"),
						Status:     res.Status,
						Tokens:     res.Tokens,
					}); err == nil {
						changed = true
					}
				}
				continue
			}
			// No native usage counters and no fallback visible text -> explicitly record no_usage.
			r.store.RecordError(r.spec.ID+"_no_usage", rowID)
			continue
		}
		ts, err := pricing.ParseTimestamp(rawEv.OccurredAt)
		if err != nil || ts.UnixMilli() < activationMs {
			continue
		}

		res, err := r.store.InsertEvent(*rawEv)
		if err != nil {
			r.classifySQLiteError(err, rowID)
			continue
		}
		if res == "inserted" {
			changed = true
		}
	}
	if err := rows.Err(); err != nil {
		return false
	}

	if rowCount > 0 {
		cur.At = lastAt
		cur.ID = lastID
		if curBytes, err := json.Marshal(cur); err == nil {
			_, _ = r.store.SQL.Exec(
				`INSERT INTO cursors(path, data) VALUES (?, ?) ON CONFLICT(path) DO UPDATE SET data=excluded.data`,
				r.path, string(curBytes),
			)
		}
	}

	caughtUp := rowCount < 200
	r.mu.Lock()
	r.sigCache = &sqliteSigCache{key: sig.Key, caughtUp: caughtUp, at: now}
	r.draining = !caughtUp
	r.status = CollectorStatus{
		State:       "running",
		Version:     r.spec.ExpectedVersion,
		LastScan:    nowISO,
		RecordsRead: rowCount,
	}
	r.mu.Unlock()

	if changed {
		r.onChange()
	}
	return changed
}

func (r *SQLiteReader) classifySQLiteError(err error, rowID string) {
	msg := err.Error()
	prefix := r.spec.ID + "_"
	if strings.HasPrefix(msg, prefix) {
		r.store.RecordError(msg, rowID)
		return
	}
	if errors.Is(err, pricing.ErrMissingMetric) {
		r.store.RecordError(r.spec.ID+"_missing_metric", rowID)
		return
	}
	if errors.Is(err, pricing.ErrInvalidBreakdown) {
		r.store.RecordError("invalid_breakdown", rowID)
		return
	}
	r.store.RecordError(r.spec.ID+"_invalid_usage", rowID)
}
