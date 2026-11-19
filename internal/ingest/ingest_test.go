package ingest

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/soyaelithia-ui/ai-usage-monitor/internal/config"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/db"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/security"
	_ "modernc.org/sqlite"
)

func TestSameSizeRewriteAndBlankLineOffset(t *testing.T) {
	now := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "mon.db"), "", func() time.Time { return now })
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer store.Close()

	logDir := filepath.Join(dir, "codex")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	logFile := filepath.Join(logDir, "session.jsonl")

	line1 := `{"type":"turn_context","payload":{"turn_id":"t1","model":"gpt-6-astra"}}` + "\n"
	line2A := `{"type":"token_usage_record","timestamp":"2026-11-18T00:01:00.000Z","payload":{"response_id":"respA","session_id":"s1","turn_id":"t1","usage":{"input_tokens":100,"output_tokens":20,"cached_input_tokens":0,"cache_write_input_tokens":0}}}` + "\n"
	line2B := `{"type":"token_usage_record","timestamp":"2026-11-18T00:01:00.000Z","payload":{"response_id":"respB","session_id":"s1","turn_id":"t1","usage":{"input_tokens":100,"output_tokens":20,"cached_input_tokens":0,"cache_write_input_tokens":0}}}` + "\n"

	if len(line2A) != len(line2B) {
		t.Fatalf("test lines must have identical byte length: %d != %d", len(line2A), len(line2B))
	}

	col, err := NewCollector(store, logDir, CodexSpec(), nil)
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}

	contentA := line1 + "\n\n" + line2A
	if err := os.WriteFile(logFile, []byte(contentA), 0o644); err != nil {
		t.Fatalf("WriteFile A: %v", err)
	}
	fixedTime := time.Date(2026, 11, 18, 0, 1, 0, 0, time.UTC)
	_ = os.Chtimes(logFile, fixedTime, fixedTime)

	if !col.Scan() {
		t.Fatal("expected initial scan to ingest event respA")
	}

	// Rewrite exact same byte length AND restore exact same mtime/atime
	contentB := line1 + "\n\n" + line2B
	if err := os.WriteFile(logFile, []byte(contentB), 0o644); err != nil {
		t.Fatalf("WriteFile B: %v", err)
	}
	_ = os.Chtimes(logFile, fixedTime, fixedTime)

	if !col.Scan() {
		t.Fatal("expected same-size rewrite with identical timestamp to be detected and ingested")
	}

	var count int
	if err := store.SQL.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("expected 2 ingested events (respA + respB), got %d (err=%v)", count, err)
	}

	// Blank-line byte offset tracking: append 2 blank lines + bad JSON with marker
	badLine := `{"type":"token_usage_record","bad_json` + "\n"
	prevSize := int64(len(contentB))
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open append: %v", err)
	}
	_, _ = f.WriteString("\n\n" + badLine)
	_ = f.Close()

	col.Scan()
	expectedOffset := prevSize + 2 // two '\n' blank lines
	expectedErrID := security.LegacyHash(fmt.Sprintf("invalid_usage_record%s:%d", logFile, expectedOffset))
	var gotKind string
	if err := store.SQL.QueryRow(`SELECT kind FROM errors WHERE id=?`, expectedErrID).Scan(&gotKind); err != nil || gotKind != "invalid_usage_record" {
		t.Fatalf("expected invalid_usage_record error at exact byte offset %d (id=%s), got %q (err=%v)", expectedOffset, expectedErrID, gotKind, err)
	}
}

func TestSymlinkAndFIFOSafetyAndFsnotify(t *testing.T) {
	now := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "mon.db"), "", func() time.Time { return now })
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer store.Close()

	logDir := filepath.Join(dir, "codex")
	_ = os.MkdirAll(logDir, 0o755)

	secretFile := filepath.Join(dir, "secret.jsonl")
	_ = os.WriteFile(secretFile, []byte(`{"type":"token_usage_record"}`+"\n"), 0o644)
	_ = os.Symlink(secretFile, filepath.Join(logDir, "link.jsonl"))

	fifoPath := filepath.Join(logDir, "pipe.jsonl")
	if err := syscall.Mkfifo(fifoPath, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}

	var changes atomic.Int32
	col, err := NewCollector(store, logDir, CodexSpec(), func() {
		changes.Add(1)
	})
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}
	col.Start()
	defer col.Stop()

	// Ensure symlink and FIFO were skipped without blocking or erroring
	if col.Status().Files != 0 {
		t.Fatalf("expected 0 regular files, got %d", col.Status().Files)
	}

	// Write a real log file and verify fsnotify picks it up automatically
	realLog := filepath.Join(logDir, "live.jsonl")
	payload := `{"type":"turn_context","payload":{"turn_id":"t9","model":"gpt-6-astra"}}` + "\n" +
		`{"type":"token_usage_record","timestamp":"2026-11-18T00:02:00.000Z","payload":{"response_id":"live1","session_id":"s9","turn_id":"t9","usage":{"input_tokens":50,"output_tokens":10,"cached_input_tokens":0,"cache_write_input_tokens":0}}}` + "\n"
	if err := os.WriteFile(realLog, []byte(payload), 0o644); err != nil {
		t.Fatalf("WriteFile live: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if changes.Load() > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if changes.Load() == 0 {
		t.Fatal("expected fsnotify to trigger ingestion on new JSONL file")
	}
}

func TestSQLiteReaderOpenCodeEpochMsAndErrorClassification(t *testing.T) {
	now := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "mon.db"), "", func() time.Time { return now })
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer store.Close()

	ocPath := filepath.Join(dir, "opencode.db")
	ocDB, err := sql.Open("sqlite", ocPath)
	if err != nil {
		t.Fatalf("open opencode db: %v", err)
	}
	_, err = ocDB.Exec(`
		CREATE TABLE session_v2 (id TEXT PRIMARY KEY, version TEXT);
		CREATE TABLE session_message (id TEXT PRIMARY KEY, session_id TEXT, time_updated INTEGER, data TEXT, type TEXT);
		INSERT INTO session_v2 VALUES ('sess1', '2.1.4');
	`)
	if err != nil {
		t.Fatalf("create opencode schema: %v", err)
	}

	tsMs := now.Add(time.Minute).UnixMilli()
	// 1. Valid record with huge >2^53 numeric ID in JSON + matching string row ID
	validJSON := `{"id":900719925474099312345,"model":{"id":"gpt-6-astra","providerID":"openai"},"time":{"completed":` +
		strconvFormatInt64(tsMs) + `},"tokens":{"input":100,"output":20,"cache":{"read":10,"write":0},"reasoning":5}}`
	_, _ = ocDB.Exec(`INSERT INTO session_message VALUES ('900719925474099312345', 'sess1', ?, ?, 'assistant')`, tsMs, validJSON)

	// 2. Missing metric record -> opencode_missing_metric
	missingMetricJSON := `{"id":"m2","model":{"id":"gpt-6-astra","providerID":"openai"},"time":{"completed":` +
		strconvFormatInt64(tsMs+1) + `},"tokens":{"output":20}}`
	_, _ = ocDB.Exec(`INSERT INTO session_message VALUES ('m2', 'sess1', ?, ?, 'assistant')`, tsMs+1, missingMetricJSON)

	// 3. No usage record -> opencode_no_usage
	noUsageJSON := `{"id":"m3","model":{"id":"gpt-6-astra","providerID":"openai"},"time":{"completed":` +
		strconvFormatInt64(tsMs+2) + `}}`
	_, _ = ocDB.Exec(`INSERT INTO session_message VALUES ('m3', 'sess1', ?, ?, 'assistant')`, tsMs+2, noUsageJSON)

	_ = ocDB.Close()

	rdr := NewSQLiteReader(store, ocPath, OpenCodeSpec(), nil)
	if !rdr.Scan() {
		t.Fatal("expected SQLiteReader.Scan to ingest valid event")
	}

	// Verify no -shm sidecar was created on passive read-only inspection
	if _, err := os.Stat(ocPath + "-shm"); !os.IsNotExist(err) {
		t.Fatal("passive read-only SQLite reader must not create -shm file")
	}

	var evCount int
	_ = store.SQL.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&evCount)
	if evCount != 1 {
		t.Fatalf("expected 1 valid event, got %d", evCount)
	}

	var dataJSON string
	_ = store.SQL.QueryRow(`SELECT data FROM events LIMIT 1`).Scan(&dataJSON)
	if !strings.Contains(dataJSON, "900719925474099312345") {
		t.Fatalf("expected canonical huge responseId preserved, got %s", dataJSON)
	}

	// Verify explicit error classifications (no silent drops)
	for _, wantKind := range []string{"opencode_missing_metric", "opencode_no_usage"} {
		var c int
		_ = store.SQL.QueryRow(`SELECT COUNT(*) FROM errors WHERE kind=?`, wantKind).Scan(&c)
		if c != 1 {
			t.Fatalf("expected error %s recorded once, got %d", wantKind, c)
		}
	}
	_ = url.PathEscape
}

func TestSilentHiddenTokenizerFallbackAndReconciliationIsolation(t *testing.T) {
	now := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "mon.db"), "", func() time.Time { return now })
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer store.Close()

	// 1. JSONL Collector fallback (Claude record with visible text and no native usage)
	claudeDir := filepath.Join(dir, "claude")
	_ = os.MkdirAll(claudeDir, 0o755)
	claudeLog := filepath.Join(claudeDir, "chat.jsonl")
	textOnlyLine := `{"type":"assistant","timestamp":"2026-11-19T10:01:00.000Z","sessionId":"cs1","message":{"id":"cl-msg-1","model":"claude-sonnet-4.6","content":[{"type":"text","text":"Silent reconciliation measurement without native counters."}]}}` + "\n"
	_ = os.WriteFile(claudeLog, []byte(textOnlyLine), 0o644)

	col, err := NewCollector(store, claudeDir, ClaudeSpec(), nil)
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}
	if !col.Scan() {
		t.Fatal("expected Collector.Scan to save silent fallback text measurement")
	}

	// 2. OpenCode SQLiteReader fallback (assistant message with content and no tokens)
	ocPath := filepath.Join(dir, "opencode.db")
	ocDB, err := sql.Open("sqlite", ocPath)
	if err != nil {
		t.Fatalf("open opencode db: %v", err)
	}
	_, _ = ocDB.Exec(`
		CREATE TABLE session_v2 (id TEXT PRIMARY KEY, version TEXT);
		CREATE TABLE session_message (id TEXT PRIMARY KEY, session_id TEXT, time_updated INTEGER, data TEXT, type TEXT);
		INSERT INTO session_v2 VALUES ('sess-fb', '2.1.4');
	`)
	tsMs := now.Add(2 * time.Minute).UnixMilli()
	fbJSON := `{"id":"oc-fb-1","model":{"id":"deepseek-v4-pro","providerID":"deepseek"},"time":{"completed":` +
		strconvFormatInt64(tsMs) + `},"content":"Texto visible medido en silencio para conciliación."}`
	_, _ = ocDB.Exec(`INSERT INTO session_message VALUES ('oc-fb-1', 'sess-fb', ?, ?, 'assistant')`, tsMs, fbJSON)
	_ = ocDB.Close()

	rdr := NewSQLiteReader(store, ocPath, OpenCodeSpec(), nil)
	if !rdr.Scan() {
		t.Fatal("expected SQLiteReader.Scan to save silent fallback text measurement")
	}

	// Verify ZERO events were inserted and native analytics remains 0 tokens
	var evCount int
	_ = store.SQL.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&evCount)
	if evCount != 0 {
		t.Fatalf("fallback text measurements must never be inserted into events, got %d", evCount)
	}
	filters, err := db.ParseFilters(url.Values{"window": {"24h"}}, now.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("ParseFilters: %v", err)
	}
	ana, err := store.QueryAnalytics(filters)
	if err != nil {
		t.Fatalf("QueryAnalytics: %v", err)
	}
	if ana.Totals.TotalTokens != "0" || ana.Totals.Count != 0 {
		t.Fatalf("fallback text measurements must never be summed into native analytics: %+v", ana.Totals)
	}

	// Verify reconciliation returns both measurements with ZERO tokenizer/engine/vocabulary keys
	rec, err := store.QueryReconciliation(filters)
	if err != nil {
		t.Fatalf("QueryReconciliation: %v", err)
	}
	if len(rec.TextMeasurements) != 2 {
		t.Fatalf("expected 2 text measurements in reconciliation, got %d", len(rec.TextMeasurements))
	}
	rawRec, _ := json.Marshal(rec)
	for _, forbidden := range []string{"tokenizer", "vocabulary", "tiktoken", "sentencepiece", "engine"} {
		if strings.Contains(strings.ToLower(string(rawRec)), forbidden) {
			t.Fatalf("reconciliation payload leaked forbidden internal field %q: %s", forbidden, rawRec)
		}
	}

	// Verify native event for the same source:responseId automatically removes its fallback text measurement
	nativeLine := `{"type":"assistant","timestamp":"2026-11-19T10:03:00.000Z","sessionId":"cs1","message":{"id":"cl-msg-1","model":"claude-sonnet-4.6","usage":{"input_tokens":50,"output_tokens":15,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}` + "\n"
	f, _ := os.OpenFile(claudeLog, os.O_APPEND|os.O_WRONLY, 0o644)
	_, _ = f.WriteString(nativeLine)
	_ = f.Close()

	if !col.Scan() {
		t.Fatal("expected native event scan")
	}
	recAfter, _ := store.QueryReconciliation(filters)
	if len(recAfter.TextMeasurements) != 1 {
		t.Fatalf("expected fallback measurement for cl-msg-1 removed once native counter arrived, got %d", len(recAfter.TextMeasurements))
	}
}

func TestDiscoverLocalTechnologiesAllEightAndSymlinkSafety(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("PATH", "") // isolate from host binaries

	// Clear any explicit env overrides
	for _, k := range []string{"CODEX_ROOT", "OPENCODE_DB", "CLAUDE_ROOT", "ANTIGRAVITY_ROOT", "GEMINI_ROOT", "CURSOR_ROOT", "COPILOT_ROOT", "HERMES_ROOT"} {
		t.Setenv(k, "")
	}

	// 1. Only create ~/.gemini/antigravity -> antigravity detected, gemini NOT falsely detected
	_ = os.MkdirAll(filepath.Join(home, ".gemini", "antigravity"), 0o755)
	// Also place a symlink for cursor -> must be ignored (O_NOFOLLOW / Lstat)
	realCursor := filepath.Join(home, "real_cursor")
	_ = os.MkdirAll(realCursor, 0o755)
	_ = os.Symlink(realCursor, filepath.Join(home, ".cursor"))

	initial := DiscoverLocalTechnologies(&config.Config{})
	byID := map[string]DiscoveredTech{}
	for _, tech := range initial {
		byID[tech.ID] = tech
	}
	if len(initial) != 8 {
		t.Fatalf("expected all 8 UI technologies returned, got %d", len(initial))
	}
	if !byID["antigravity"].Detected {
		t.Fatal("expected antigravity detected")
	}
	if byID["gemini"].Detected {
		t.Fatal("~/.gemini/antigravity must not falsely detect gemini")
	}
	if byID["cursor"].Detected {
		t.Fatal("symlink ~/.cursor must be ignored")
	}

	// 2. Populate regular paths for all 8 technologies via config
	cfg := &config.Config{
		CodexRoot:       filepath.Join(home, "r_codex"),
		OpenCodeDB:      filepath.Join(home, "r_opencode.db"),
		ClaudeRoot:      filepath.Join(home, "r_claude"),
		AntigravityRoot: filepath.Join(home, "r_antigravity"),
		GeminiRoot:      filepath.Join(home, "r_gemini"),
		CursorRoot:      filepath.Join(home, "r_cursor"),
		CopilotRoot:     filepath.Join(home, "r_copilot"),
		HermesRoot:      filepath.Join(home, "r_hermes"),
	}
	for _, d := range []string{cfg.CodexRoot, cfg.ClaudeRoot, cfg.AntigravityRoot, cfg.GeminiRoot, cfg.CursorRoot, cfg.CopilotRoot, cfg.HermesRoot} {
		_ = os.MkdirAll(d, 0o755)
	}
	_ = os.WriteFile(cfg.OpenCodeDB, []byte(""), 0o644)

	all := DiscoverLocalTechnologies(cfg)
	for _, tech := range all {
		if !tech.Detected || !tech.Configured {
			t.Errorf("expected tech %q detected & configured, got %+v", tech.ID, tech)
		}
	}
}

func strconvFormatInt64(v int64) string {
	return time.UnixMilli(0).Add(time.Duration(v) * time.Millisecond).Format("")[:0] + itoa64(v)
}

func itoa64(v int64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := v < 0
	if neg {
		v = -v
	}
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
