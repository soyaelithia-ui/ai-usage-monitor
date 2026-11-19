package db

const schemaSQL = `
CREATE TABLE IF NOT EXISTS meta(
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS events(
    id INTEGER PRIMARY KEY,
    identity TEXT UNIQUE NOT NULL,
    fingerprint TEXT NOT NULL,
    occurred_at INTEGER NOT NULL,
    agent TEXT NOT NULL,
    model TEXT NOT NULL,
    data TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS events_time ON events(occurred_at, agent, model);
CREATE TABLE IF NOT EXISTS cursors(
    path TEXT PRIMARY KEY,
    data TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS contexts(
    turn TEXT PRIMARY KEY,
    model TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS errors(
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS accesses(
    hash TEXT PRIMARY KEY,
    expires INTEGER NOT NULL,
    used INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS sessions(
    hash TEXT PRIMARY KEY,
    expires INTEGER NOT NULL,
    csrf TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS native_observations(
    identity TEXT PRIMARY KEY,
    fingerprint TEXT NOT NULL,
    observed_at TEXT NOT NULL,
    data TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS text_measurements(
    identity TEXT PRIMARY KEY,
    occurred_at INTEGER NOT NULL,
    data TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS attempts(
    key TEXT PRIMARY KEY,
    count INTEGER NOT NULL,
    until INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS dynamic_agents(
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    reader TEXT NOT NULL,
    provider TEXT,
    tone TEXT,
    symbol TEXT,
    config_json TEXT,
    created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS dynamic_pricing(
    model TEXT PRIMARY KEY,
    provider TEXT,
    input TEXT NOT NULL,
    cached TEXT NOT NULL,
    output TEXT NOT NULL,
    cache_write TEXT,
    cache_write_12h TEXT,
    source TEXT NOT NULL DEFAULT 'agent'
);
`
