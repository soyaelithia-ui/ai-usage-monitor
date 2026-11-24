package db

import (
	"database/sql"
	"errors"
	"time"

	"github.com/soyaelithia-ui/ai-usage-monitor/internal/security"
)

const (
	SessionTTL           = 8 * time.Hour
	PermanentSessionTTL  = 365 * 24 * time.Hour
	ExchangeMaxAttempts  = 20
	ExchangeAttemptWinMs = 60_000
)

// IssuedToken represents a newly minted access token.
type IssuedToken struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expiresAt"`
	Permanent bool   `json:"permanent,omitempty"`
	Reusable  bool   `json:"reusable,omitempty"`
}

// IssueOptions configures optional attributes for minted access tokens.
type IssueOptions struct {
	Permanent    bool
	Reusable     bool
	TTL          time.Duration
	KeepExisting bool
}

// SessionInfo represents an active authenticated session.
type SessionInfo struct {
	SessionHash string
	RawSession  string
	CSRF        string
	ExpiresMs   int64
	ExpiresAt   string
	Permanent   bool
}

// Issue revokes all previous accesses/sessions and issues a fresh 8h one-time token.
func (s *Store) Issue() (*IssuedToken, error) {
	return s.IssueWithOptions(IssueOptions{})
}

// IssueWithOptions mints an access token with configurable permanence, reusability, TTL, and session preservation.
func (s *Store) IssueWithOptions(opts IssueOptions) (*IssuedToken, error) {
	if opts.TTL < 0 {
		return nil, errors.New("token ttl must be positive")
	}
	token, err := security.GenerateSecret()
	if err != nil {
		return nil, err
	}

	now := s.NowFn()
	var (
		expiresMs    int64
		expiresAt    string
		reusableInt  int
		permanentInt int
		sessionTTLMs int64
	)

	if opts.Permanent {
		reusableInt = 1
		permanentInt = 1
		expiresMs = 0
		expiresAt = "never"
		ttl := opts.TTL
		if ttl <= 0 {
			ttl = PermanentSessionTTL
		}
		sessionTTLMs = ttl.Milliseconds()
	} else {
		if opts.Reusable {
			reusableInt = 1
		}
		ttl := opts.TTL
		if ttl <= 0 {
			ttl = SessionTTL
		} else {
			sessionTTLMs = ttl.Milliseconds()
		}
		expiresMs = now.Add(ttl).UnixMilli()
		expiresAt = time.UnixMilli(expiresMs).UTC().Format("2006-01-02T15:04:05.000Z")
	}

	tx, err := s.SQL.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	if !opts.KeepExisting {
		if _, err := tx.Exec(`DELETE FROM accesses`); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(`DELETE FROM sessions`); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(
		`INSERT INTO accesses(hash, expires, used, reusable, permanent, session_ttl_ms) VALUES (?, ?, 0, ?, ?, ?)`,
		s.Sealer.Seal(token), expiresMs, reusableInt, permanentInt, sessionTTLMs,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &IssuedToken{
		Token:     token,
		ExpiresAt: expiresAt,
		Permanent: permanentInt != 0,
		Reusable:  reusableInt != 0,
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

// Exchange consumes a one-time token (or validates a reusable token) and creates a bound session + CSRF token.
// Returns (nil, nil) if the token is invalid, expired, or already used.
func (s *Store) Exchange(token string) (*SessionInfo, error) {
	return s.ExchangeWithDefaultTTL(token, 0)
}

// ExchangeWithDefaultTTL exchanges an access token using session_ttl_ms if set, otherwise defaultTTL (or SessionTTL).
func (s *Store) ExchangeWithDefaultTTL(token string, defaultTTL time.Duration) (*SessionInfo, error) {
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

	var (
		permanentInt int
		sessionTTLMs int64
	)

	// Single-use tokens must be consumed atomically.
	err = tx.QueryRow(
		`UPDATE accesses SET used=1 WHERE hash=? AND reusable=0 AND used=0 AND (expires=0 OR expires>?)
		 RETURNING permanent, session_ttl_ms`,
		sealedToken, nowMs,
	).Scan(&permanentInt, &sessionTTLMs)
	if errors.Is(err, sql.ErrNoRows) {
		// Migrate legacy plain-SHA256 single-use access row once if present.
		err = tx.QueryRow(
			`UPDATE accesses SET used=1, hash=? WHERE hash=? AND reusable=0 AND used=0 AND (expires=0 OR expires>?)
			 RETURNING permanent, session_ttl_ms`,
			sealedToken, legacyToken, nowMs,
		).Scan(&permanentInt, &sessionTTLMs)
	}
	if errors.Is(err, sql.ErrNoRows) {
		// Reusable/permanent tokens are validated without setting used=1.
		err = tx.QueryRow(
			`SELECT permanent, session_ttl_ms FROM accesses
			 WHERE hash=? AND reusable=1 AND used=0 AND (expires=0 OR expires>?)`,
			sealedToken, nowMs,
		).Scan(&permanentInt, &sessionTTLMs)
		if errors.Is(err, sql.ErrNoRows) {
			err = tx.QueryRow(
				`UPDATE accesses SET hash=? WHERE hash=? AND reusable=1 AND used=0 AND (expires=0 OR expires>?)
				 RETURNING permanent, session_ttl_ms`,
				sealedToken, legacyToken, nowMs,
			).Scan(&permanentInt, &sessionTTLMs)
		}
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var effectiveTTLMs int64
	switch {
	case sessionTTLMs > 0:
		effectiveTTLMs = sessionTTLMs
	case defaultTTL > 0:
		effectiveTTLMs = defaultTTL.Milliseconds()
	case permanentInt != 0:
		effectiveTTLMs = PermanentSessionTTL.Milliseconds()
	default:
		effectiveTTLMs = SessionTTL.Milliseconds()
	}

	sess, err := s.insertSessionTx(tx, nowMs+effectiveTTLMs, permanentInt != 0)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return sess, nil
}

// CreateSession creates a new authenticated session with the given TTL (defaulting to SessionTTL if <= 0).
func (s *Store) CreateSession(ttl time.Duration) (*SessionInfo, error) {
	if ttl <= 0 {
		ttl = SessionTTL
	}
	nowMs := s.NowFn().UnixMilli()
	tx, err := s.SQL.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	sess, err := s.insertSessionTx(tx, nowMs+ttl.Milliseconds(), false)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return sess, nil
}

func (s *Store) insertSessionTx(tx *sql.Tx, expiresMs int64, permanent bool) (*SessionInfo, error) {
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
	return &SessionInfo{
		SessionHash: sessionHash,
		RawSession:  rawSession,
		CSRF:        csrf,
		ExpiresMs:   expiresMs,
		ExpiresAt:   time.UnixMilli(expiresMs).UTC().Format("2006-01-02T15:04:05.000Z"),
		Permanent:   permanent,
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
