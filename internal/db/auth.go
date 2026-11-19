package db

import (
	"database/sql"
	"errors"
	"time"

	"github.com/soyaelithia-ui/ai-usage-monitor/internal/security"
)

const (
	SessionTTL           = 8 * time.Hour
	ExchangeMaxAttempts  = 20
	ExchangeAttemptWinMs = 60_000
)

// IssuedToken represents a newly minted one-time access token.
type IssuedToken struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expiresAt"`
}

// SessionInfo represents an active authenticated session.
type SessionInfo struct {
	SessionHash string
	RawSession  string
	CSRF        string
	ExpiresMs   int64
	ExpiresAt   string
}

// Issue revokes all previous accesses/sessions and issues a fresh 8h one-time token.
func (s *Store) Issue() (*IssuedToken, error) {
	token, err := security.GenerateSecret()
	if err != nil {
		return nil, err
	}
	now := s.NowFn()
	expiresMs := now.Add(SessionTTL).UnixMilli()

	tx, err := s.SQL.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM accesses`); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`DELETE FROM sessions`); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`INSERT INTO accesses(hash, expires, used) VALUES (?, ?, 0)`, s.Sealer.Seal(token), expiresMs); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &IssuedToken{
		Token:     token,
		ExpiresAt: time.UnixMilli(expiresMs).UTC().Format("2006-01-02T15:04:05.000Z"),
	}, nil
}

// Revoke deletes all access tokens and active sessions.
func (s *Store) Revoke() error {
	tx, err := s.SQL.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM accesses`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM sessions`); err != nil {
		return err
	}
	return tx.Commit()
}

// CheckAndRecordExchangeAttempt enforces a 20-attempts-per-minute rate limit for /auth/exchange.
// Returns true if allowed, false if rate-limited.
func (s *Store) CheckAndRecordExchangeAttempt() (bool, error) {
	nowMs := s.NowFn().UnixMilli()
	tx, err := s.SQL.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	var count int
	var until int64
	err = tx.QueryRow(`SELECT count, until FROM attempts WHERE key='exchange'`).Scan(&count, &until)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if err == nil && until > nowMs && count >= ExchangeMaxAttempts {
		return false, nil
	}

	nextCount := 1
	nextUntil := nowMs + ExchangeAttemptWinMs
	if err == nil && until > nowMs {
		nextCount = count + 1
		nextUntil = until
	}
	if _, err := tx.Exec(
		`INSERT INTO attempts(key, count, until) VALUES ('exchange', ?, ?)
		 ON CONFLICT(key) DO UPDATE SET count=excluded.count, until=excluded.until`,
		nextCount, nextUntil,
	); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// Exchange atomically consumes a one-time access token and creates a bound session + CSRF token.
// Returns (nil, nil) if the token is invalid, expired, or already used.
func (s *Store) Exchange(token string) (*SessionInfo, error) {
	if !security.SecretRegexp.MatchString(token) {
		return nil, nil
	}
	nowMs := s.NowFn().UnixMilli()
	sealedToken := s.Sealer.Seal(token)
	legacyToken := security.LegacyHash(token)

	tx, err := s.SQL.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var expiresMs int64
	err = tx.QueryRow(
		`UPDATE accesses SET used=1 WHERE hash=? AND used=0 AND expires>? RETURNING expires`,
		sealedToken, nowMs,
	).Scan(&expiresMs)
	if errors.Is(err, sql.ErrNoRows) {
		// Migrate legacy plain-SHA256 access row once if present.
		err = tx.QueryRow(
			`UPDATE accesses SET used=1, hash=? WHERE hash=? AND used=0 AND expires>? RETURNING expires`,
			sealedToken, legacyToken, nowMs,
		).Scan(&expiresMs)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	rawSession, err := security.GenerateSecret()
	if err != nil {
		return nil, err
	}
	sessionHash := s.Sealer.Seal(rawSession)
	csrf := s.Sealer.Seal(rawSession)
	if _, err := tx.Exec(
		`INSERT INTO sessions(hash, expires, csrf) VALUES (?, ?, ?)`,
		sessionHash, expiresMs, csrf,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &SessionInfo{
		SessionHash: sessionHash,
		RawSession:  rawSession,
		CSRF:        csrf,
		ExpiresMs:   expiresMs,
		ExpiresAt:   time.UnixMilli(expiresMs).UTC().Format("2006-01-02T15:04:05.000Z"),
	}, nil
}

// LookupSession validates a raw session cookie token and returns its SessionInfo if active.
// Transparently migrates legacy plain SHA-256 session rows to HMAC-sealed form.
func (s *Store) LookupSession(rawSession string) (*SessionInfo, error) {
	if !security.SecretRegexp.MatchString(rawSession) {
		return nil, nil
	}
	nowMs := s.NowFn().UnixMilli()
	sealed := s.Sealer.Seal(rawSession)

	var expiresMs int64
	var csrf string
	err := s.SQL.QueryRow(
		`SELECT expires, csrf FROM sessions WHERE hash=? AND expires>?`,
		sealed, nowMs,
	).Scan(&expiresMs, &csrf)
	if err == nil {
		return &SessionInfo{
			SessionHash: sealed,
			RawSession:  rawSession,
			CSRF:        s.Sealer.Seal(rawSession),
			ExpiresMs:   expiresMs,
			ExpiresAt:   time.UnixMilli(expiresMs).UTC().Format("2006-01-02T15:04:05.000Z"),
		}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	legacy := security.LegacyHash(rawSession)
	err = s.SQL.QueryRow(
		`SELECT expires, csrf FROM sessions WHERE hash=? AND expires>?`,
		legacy, nowMs,
	).Scan(&expiresMs, &csrf)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// Migrate legacy row to sealed form and derived CSRF.
	if _, uErr := s.SQL.Exec(
		`UPDATE sessions SET hash=?, csrf=? WHERE hash=?`,
		sealed, sealed, legacy,
	); uErr != nil {
		return nil, uErr
	}
	return &SessionInfo{
		SessionHash: sealed,
		RawSession:  rawSession,
		CSRF:        sealed,
		ExpiresMs:   expiresMs,
		ExpiresAt:   time.UnixMilli(expiresMs).UTC().Format("2006-01-02T15:04:05.000Z"),
	}, nil
}

// IsSessionHashActive checks whether a sealed session hash still exists and has not expired.
func (s *Store) IsSessionHashActive(sessionHash string) bool {
	if sessionHash == "" {
		return false
	}
	nowMs := s.NowFn().UnixMilli()
	var dummy int
	err := s.SQL.QueryRow(`SELECT 1 FROM sessions WHERE hash=? AND expires>?`, sessionHash, nowMs).Scan(&dummy)
	return err == nil
}

// DeleteSession deletes the session matching rawSession (logout).
func (s *Store) DeleteSession(rawSession string) (string, error) {
	sealed := s.Sealer.Seal(rawSession)
	_, err := s.SQL.Exec(`DELETE FROM sessions WHERE hash=?`, sealed)
	return sealed, err
}

// CSRFFromSession returns the derived synchronizer CSRF token for rawSession.
func (s *Store) CSRFFromSession(rawSession string) string {
	return s.Sealer.Seal(rawSession)
}
