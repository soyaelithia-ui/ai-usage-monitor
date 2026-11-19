package ingest

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/db"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/pricing"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/tokenizer"
)

const (
	MaxChunkBytes  = 16 * 1024 * 1024
	MaxWatches     = 32
	WatchDebounce  = 250 * time.Millisecond
	RescanInterval = 60 * time.Second
)

type jsonlCursor struct {
	Offset       int64  `json:"offset"`
	Inode        string `json:"inode"`
	Sig          string `json:"sig"`
	MtimeNs      int64  `json:"mtimeNs"`
	ObservedSize int64  `json:"observedSize"`
}

// CollectorStatus describes the current state of a reader.
type CollectorStatus struct {
	State       string `json:"state"`
	Reason      string `json:"reason,omitempty"`
	LastScan    string `json:"lastScan,omitempty"`
	LastEvent   string `json:"lastEvent,omitempty"`
	Files       int    `json:"files,omitempty"`
	RecordsRead int    `json:"recordsRead,omitempty"`
	Version     string `json:"version,omitempty"`
}

// Collector passively tails JSONL agent logs using fsnotify and bounded reads.
type Collector struct {
	store    *db.Store
	root     string
	spec     AgentSpec
	pattern  *regexp.Regexp
	counter  *tokenizer.Counter
	onChange func()

	mu           sync.Mutex
	status       CollectorStatus
	fileSig      map[string]string
	fileContent  map[string]string
	fileDraining map[string]bool
	scanning     bool
	scanPending  bool
	scanFailed   bool

	watcher    *fsnotify.Watcher
	watchedDir map[string]struct{}
	stopCh     chan struct{}
	debounceMu sync.Mutex
	debounce   *time.Timer
}

// NewCollector constructs a JSONL Collector for an AgentSpec and root directory.
func NewCollector(store *db.Store, root string, spec AgentSpec, onChange func()) (*Collector, error) {
	return NewCollectorWithCounter(store, root, spec, tokenizer.NewCounter(""), onChange)
}

// NewCollectorWithCounter constructs a JSONL Collector with a shared hidden tokenizer Counter.
func NewCollectorWithCounter(store *db.Store, root string, spec AgentSpec, counter *tokenizer.Counter, onChange func()) (*Collector, error) {
	patStr := spec.FilePattern
	if patStr == "" {
		patStr = `\.jsonl$`
	}
	pat, err := regexp.Compile(patStr)
	if err != nil {
		return nil, err
	}
	if counter == nil {
		counter = tokenizer.NewCounter("")
	}
	if onChange == nil {
		onChange = func() {}
	}
	return &Collector{
		store:        store,
		root:         root,
		spec:         spec,
		pattern:      pat,
		counter:      counter,
		onChange:     onChange,
		status:       CollectorStatus{State: "starting"},
		fileSig:      make(map[string]string),
		fileContent:  make(map[string]string),
		fileDraining: make(map[string]bool),
		watchedDir:   make(map[string]struct{}),
		stopCh:       make(chan struct{}),
	}, nil
}

// Status returns a snapshot of the collector's status.
func (c *Collector) Status() CollectorStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

// Start begins watching and performs an initial scan.
func (c *Collector) Start() {
	if c.root != "" {
		var dummy int
		err := c.store.SQL.QueryRow(`SELECT 1 FROM meta WHERE key='provenance_v2'`).Scan(&dummy)
		if errors.Is(err, sql.ErrNoRows) {
			tx, txErr := c.store.SQL.Begin()
			if txErr == nil {
				_, _ = tx.Exec(`DELETE FROM cursors WHERE path LIKE ?`, c.root+"/%")
				_, _ = tx.Exec(`INSERT OR IGNORE INTO meta(key, value) VALUES ('provenance_v2', '1')`)
				_ = tx.Commit()
			}
		}
	}

	if c.root != "" {
		if info, err := os.Lstat(c.root); err == nil && info.IsDir() {
			if w, err := fsnotify.NewWatcher(); err == nil {
				c.watcher = w
				go c.watchLoop()
			}
		}
	}

	c.Scan()

	go func() {
		ticker := time.NewTicker(RescanInterval)
		defer ticker.Stop()
		for {
			select {
			case <-c.stopCh:
				return
			case <-ticker.C:
				c.Scan()
			}
		}
	}()
}

// Stop halts watchers and background timers.
func (c *Collector) Stop() {
	select {
	case <-c.stopCh:
		return
	default:
		close(c.stopCh)
	}
	c.debounceMu.Lock()
	if c.debounce != nil {
		c.debounce.Stop()
	}
	c.debounceMu.Unlock()
	if c.watcher != nil {
		_ = c.watcher.Close()
	}
}

func (c *Collector) scheduleScan(delay time.Duration) {
	c.debounceMu.Lock()
	defer c.debounceMu.Unlock()
	if c.debounce != nil {
		c.debounce.Stop()
	}
	c.debounce = time.AfterFunc(delay, func() {
		select {
		case <-c.stopCh:
			return
		default:
			c.Scan()
		}
	})
}

func (c *Collector) watchLoop() {
	for {
		select {
		case <-c.stopCh:
			return
		case ev, ok := <-c.watcher.Events:
			if !ok {
				return
			}
			rel, _ := filepath.Rel(c.root, ev.Name)
			if IsIgnoredPath(rel) || IsAuxiliaryFile(filepath.Base(ev.Name)) {
				continue
			}
			base := filepath.Base(ev.Name)
			dir := filepath.Dir(ev.Name)
			if c.pattern.MatchString(base) || (dir == c.root && !stringsContainsDot(base)) || ev.Has(fsnotify.Create) {
				c.scheduleScan(WatchDebounce)
			}
		case _, ok := <-c.watcher.Errors:
			if !ok {
				return
			}
		}
	}
}

func stringsContainsDot(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			return true
		}
	}
	return false
}

// Scan executes a single-flight scan of c.root.
func (c *Collector) Scan() bool {
	c.mu.Lock()
	if c.scanning {
		c.scanPending = true
		c.mu.Unlock()
		return false
	}
	c.scanning = true
	c.mu.Unlock()

	changed := c.scanBody()

	c.mu.Lock()
	c.scanning = false
	rescan := c.scanPending
	c.scanPending = false
	c.mu.Unlock()

	if rescan {
		c.scheduleScan(100 * time.Millisecond)
	}
	return changed
}

func statFingerprint(info os.FileInfo) string {
	return fmt.Sprintf("%d:%d:%d", statInode(info), info.Size(), info.ModTime().UnixNano())
}

func tailSigOfPath(path string) (string, bool) {
	f, info, err := OpenSafeReadOnly(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	sig, err := TailSignature(f, info.Size())
	if err != nil {
		return "", false
	}
	return sig, true
}

func offsetSignature(f *os.File, offset int64) string {
	if offset <= 0 {
		return ""
	}
	n := offset
	if n > 128 {
		n = 128
	}
	buf := make([]byte, n)
	readN, err := f.ReadAt(buf, offset-n)
	if err != nil && !errors.Is(err, io.EOF) {
		return ""
	}
	sum := sha256.Sum256(buf[:readN])
	return hex.EncodeToString(sum[:])
}

func (c *Collector) scanBody() bool {
	nowISO := c.store.NowFn().UTC().Format("2006-01-02T15:04:05.000Z")
	if c.root == "" {
		c.mu.Lock()
		c.status = CollectorStatus{
			State:    "waiting",
			Reason:   "Ruta de registros no configurada",
			LastScan: nowISO,
		}
		c.mu.Unlock()
		return false
	}

	linfo, err := os.Lstat(c.root)
	if err != nil || linfo.Mode()&os.ModeSymlink != 0 || !linfo.IsDir() {
		c.mu.Lock()
		c.status = CollectorStatus{
			State:    "error",
			Reason:   "Ruta de registros no disponible",
			LastScan: nowISO,
		}
		c.mu.Unlock()
		c.store.RecordError("collector_read_failed", c.root)
		return false
	}

	list, _ := WalkBounded(c.root, c.pattern)
	c.syncWatchers(list)

	changed := false
	c.scanFailed = false
	for _, p := range list {
		info, err := os.Lstat(p)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			continue
		}
		sig := statFingerprint(info)
		if sig == c.fileSig[p] && !c.fileDraining[p] {
			content, ok := tailSigOfPath(p)
			if !ok || content == c.fileContent[p] {
				continue
			}
		}
		if c.readFile(p, sig) {
			changed = true
		}
	}

	c.mu.Lock()
	c.status.Files = len(list)
	c.status.LastScan = nowISO
	if c.scanFailed {
		c.status.State = "error"
	} else {
		c.status.State = "running"
		c.status.Reason = ""
	}
	c.mu.Unlock()

	if changed {
		c.onChange()
	}
	return changed
}

func (c *Collector) syncWatchers(list []string) {
	if c.watcher == nil {
		return
	}
	want := map[string]struct{}{c.root: {}}
	for _, p := range list {
		if len(want) >= MaxWatches {
			break
		}
		want[filepath.Dir(p)] = struct{}{}
	}
	for dir := range c.watchedDir {
		if _, ok := want[dir]; !ok {
			_ = c.watcher.Remove(dir)
			delete(c.watchedDir, dir)
		}
	}
	for dir := range want {
		if _, ok := c.watchedDir[dir]; ok {
			continue
		}
		if err := c.watcher.Add(dir); err == nil {
			c.watchedDir[dir] = struct{}{}
		}
	}
}

func (c *Collector) readFile(path, sig string) bool {
	f, st, err := OpenSafeReadOnly(path)
	if err != nil {
		return false
	}
	defer f.Close()

	contentSig, err := TailSignature(f, st.Size())
	if err != nil {
		c.store.RecordError("collector_file_failed", path)
		c.scanFailed = true
		return false
	}

	inodeStr := strconv.FormatUint(statInode(st), 10)
	mtimeNs := st.ModTime().UnixNano()

	var savedRaw string
	err = c.store.SQL.QueryRow(`SELECT data FROM cursors WHERE path=?`, path).Scan(&savedRaw)
	cur := jsonlCursor{Offset: 0, Inode: inodeStr}
	if err == nil && savedRaw != "" {
		_ = json.Unmarshal([]byte(savedRaw), &cur)
	}

	prevContent, hasPrevContent := c.fileContent[path]
	resetCursor := cur.Inode != inodeStr ||
		st.Size() < cur.Offset ||
		offsetSignature(f, cur.Offset) != cur.Sig ||
		(st.Size() <= cur.Offset && hasPrevContent && contentSig != prevContent) ||
		(cur.MtimeNs != 0 && mtimeNs != cur.MtimeNs && st.Size() <= cur.ObservedSize)
	if resetCursor {
		cur = jsonlCursor{Offset: 0, Inode: inodeStr}
	}

	if st.Size() == cur.Offset {
		c.fileSig[path] = sig
		c.fileContent[path] = contentSig
		delete(c.fileDraining, path)
		return false
	}

	toRead := st.Size() - cur.Offset
	if toRead > MaxChunkBytes {
		toRead = MaxChunkBytes
	}
	buf := make([]byte, toRead)
	n, err := f.ReadAt(buf, cur.Offset)
	if err != nil && !errors.Is(err, io.EOF) {
		c.store.RecordError("collector_file_failed", path)
		c.scanFailed = true
		return false
	}
	buf = buf[:n]

	end := bytes.LastIndexByte(buf, '\n')
	if end < 0 {
		if n == MaxChunkBytes {
			c.scanFailed = true
			c.store.RecordError("oversized_or_partial_record", fmt.Sprintf("%s:%d", path, cur.Offset))
			return false
		}
		c.fileSig[path] = sig
		c.fileContent[path] = contentSig
		delete(c.fileDraining, path)
		return false
	}

	activationMs := c.store.ActivationTime().UnixMilli()
	changed := false
	byteOffset := cur.Offset

	lines := bytes.Split(buf[:end], []byte{'\n'})
	for _, lineBytes := range lines {
		lineLen := int64(len(lineBytes))
		if len(bytes.TrimSpace(lineBytes)) == 0 {
			byteOffset += lineLen + 1
			continue
		}

		if len(c.spec.Markers) > 0 {
			matched := false
			for _, m := range c.spec.Markers {
				if bytes.Contains(lineBytes, []byte(m)) {
					matched = true
					break
				}
			}
			if !matched {
				byteOffset += lineLen + 1
				continue
			}
		}

		c.processLine(path, lineBytes, byteOffset, activationMs, &changed)
		byteOffset += lineLen + 1
	}

	cur.Offset += int64(end) + 1
	cur.Sig = offsetSignature(f, cur.Offset)
	cur.MtimeNs = mtimeNs
	cur.ObservedSize = st.Size()
	if curBytes, err := json.Marshal(cur); err == nil {
		_, _ = c.store.SQL.Exec(
			`INSERT INTO cursors(path, data) VALUES (?, ?) ON CONFLICT(path) DO UPDATE SET data=excluded.data`,
			path, string(curBytes),
		)
	}

	c.fileSig[path] = sig
	c.fileContent[path] = contentSig
	if cur.Offset >= st.Size() {
		delete(c.fileDraining, path)
	} else {
		c.fileDraining[path] = true
	}
	return changed
}

func (c *Collector) processLine(path string, lineBytes []byte, byteOffset, activationMs int64, changed *bool) {
	loc := fmt.Sprintf("%s:%d", path, byteOffset)
	rec, err := ParseJSONPreservingNumbers(lineBytes)
	if err != nil {
		c.store.RecordError("invalid_usage_record", loc)
		return
	}

	recType := CanonicalID(rec["type"])
	if recType == "" {
		if pt, ok := Pluck(rec, "payload.type"); ok {
			recType = CanonicalID(pt)
		}
	}

	if c.spec.ContextRecordType != "" && recType == c.spec.ContextRecordType {
		turnVal, okT := Pluck(rec, c.spec.ContextKeyPath)
		modelVal, okM := Pluck(rec, c.spec.ContextModelPath)
		if okT && okM {
			turn := CanonicalID(turnVal)
			model := CanonicalID(modelVal)
			if turn != "" && model != "" {
				_, _ = c.store.SQL.Exec(
					`INSERT INTO contexts(turn, model) VALUES (?, ?) ON CONFLICT(turn) DO UPDATE SET model=excluded.model`,
					turn, model,
				)
			}
		}
	}

	if len(c.spec.RecordTypes) > 0 && !slices.Contains(c.spec.RecordTypes, recType) {
		return
	}

	ctx := map[string]string{}
	if c.spec.ContextRecordType != "" {
		model := "unknown"
		if turnVal, ok := Pluck(rec, c.spec.ContextKeyPath); ok {
			var foundModel string
			if c.store.SQL.QueryRow(`SELECT model FROM contexts WHERE turn=?`, CanonicalID(turnVal)).Scan(&foundModel) == nil && foundModel != "" {
				model = foundModel
			}
		}
		ctx["model"] = model
	}

	rawEv, err := CatalogMessage(c.spec, rec, ctx)
	if err != nil {
		c.classifyError(err, loc)
		return
	}
	if rawEv == nil {
		// Silent pure-Go hidden tokenizer fallback when local source delivers text without native counters.
		if fb, ok := ExtractFallbackMeta(c.spec, rec, ctx); ok && fb.OccurredAt.UnixMilli() >= activationMs {
			res := c.counter.Count(fb.Provider, fb.Model, fb.Text)
			if err := c.store.SaveTextMeasurement(db.TextMeasurement{
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
				*changed = true
			}
		}
		return
	}
	if ts, err := pricing.ParseTimestamp(rawEv.OccurredAt); err != nil || ts.UnixMilli() < activationMs {
		return
	}

	res, err := c.store.InsertEvent(*rawEv)
	if err != nil {
		c.classifyError(err, loc)
		return
	}
	if res == "inserted" {
		*changed = true
		c.mu.Lock()
		c.status.LastEvent = rawEv.OccurredAt
		c.status.RecordsRead++
		c.mu.Unlock()
	}
}

func (c *Collector) classifyError(err error, loc string) {
	msg := err.Error()
	prefix := c.spec.ID + "_"
	if len(msg) > len(prefix) && msg[:len(prefix)] == prefix {
		c.store.RecordError(msg, loc)
		return
	}
	switch {
	case errors.Is(err, pricing.ErrMissingMetric):
		c.store.RecordError(c.spec.ID+"_missing_metric", loc)
	case errors.Is(err, pricing.ErrInvalidBreakdown):
		c.store.RecordError("invalid_breakdown", loc)
	default:
		c.store.RecordError("invalid_usage_record", loc)
	}
}
