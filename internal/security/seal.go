package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// SecretRegexp matches 43-character base64url secrets (256-bit random values).
var SecretRegexp = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

// Sealer binds tokens, sessions, and CSRF values to a machine-local 256-bit key
// via HMAC-SHA256. Without machine.key, stored hashes cannot be brute-forced offline.
type Sealer struct {
	key       []byte
	ephemeral bool
}

// MachineKeyPath returns the path to machine.key next to dbPath, or overridden by env.
func MachineKeyPath(dbPath, override string) string {
	if override != "" {
		if abs, err := filepath.Abs(override); err == nil {
			return abs
		}
		return override
	}
	if env := os.Getenv("USAGE_MONITOR_MACHINE_KEY"); env != "" {
		if abs, err := filepath.Abs(env); err == nil {
			return abs
		}
		return env
	}
	if dbPath == "" || dbPath == ":memory:" || strings.HasPrefix(dbPath, "file::memory:") {
		return ""
	}
	abs, err := filepath.Abs(dbPath)
	if err != nil {
		return filepath.Join(filepath.Dir(dbPath), "machine.key")
	}
	return filepath.Join(filepath.Dir(abs), "machine.key")
}

// NewSealer loads or atomically creates a 256-bit machine key with mode 0600.
// If persistent storage is unavailable (e.g. :memory: DB), an ephemeral key is used.
func NewSealer(dbPath, keyPathOverride string) (*Sealer, error) {
	keyPath := MachineKeyPath(dbPath, keyPathOverride)
	if keyPath != "" {
		if k, err := loadOrCreateKey(keyPath); err == nil {
			return &Sealer{key: k, ephemeral: false}, nil
		}
	}
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return nil, err
	}
	return &Sealer{key: k, ephemeral: true}, nil
}

func loadOrCreateKey(path string) ([]byte, error) {
	if k, err := readMachineKey(path); err == nil {
		_ = os.Chmod(path, 0o600)
		return k, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return readMachineKey(path)
		}
		return nil, err
	}
	_, writeErr := f.WriteString(hex.EncodeToString(k) + "\n")
	closeErr := f.Close()
	if writeErr != nil {
		return nil, writeErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	_ = os.Chmod(path, 0o600)
	return k, nil
}

func readMachineKey(path string) ([]byte, error) {
	rawBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	raw := strings.TrimSpace(string(rawBytes))
	var decoded []byte
	if len(raw) == 64 {
		if b, err := hex.DecodeString(raw); err == nil {
			decoded = b
		}
	}
	if decoded == nil {
		b, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil {
			return nil, errors.New("invalid machine key encoding")
		}
		decoded = b
	}
	if len(decoded) < 32 {
		return nil, errors.New("machine key too short")
	}
	return decoded, nil
}

// IsEphemeral reports whether the sealer is using an in-memory ephemeral key.
func (s *Sealer) IsEphemeral() bool {
	return s.ephemeral
}

// Seal computes the hex-encoded HMAC-SHA256 of value using the machine key.
func (s *Sealer) Seal(value string) string {
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}

// LegacyHash computes plain hex SHA-256 (used for non-secret fingerprints and legacy migration).
func LegacyHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// SealedEqual compares Seal(candidate) to stored in constant time.
func (s *Sealer) SealedEqual(candidate, stored string) bool {
	if candidate == "" || stored == "" {
		return false
	}
	return EqualConst(s.Seal(candidate), stored)
}

// EqualConst compares two strings in constant time.
func EqualConst(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// GenerateSecret generates a 256-bit cryptographically random 43-char base64url token.
func GenerateSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
