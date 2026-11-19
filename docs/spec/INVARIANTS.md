# Architectural Invariants & Guarantee Specifications

This document defines the authoritative, inviolable architectural invariants, security perimeters, ingestion guarantees, pricing semantics, and UX/accessibility standards for **AI Usage Monitor** (Go + Svelte 5 single binary).

> [!NOTE]
> **Implementation Authority**: These invariants are enforced directly in the Go packages (`internal/security`, `internal/pricing`, `internal/tokenizer`, `internal/db`, `internal/ingest`, `internal/api`) and the Svelte 5 frontend (`web/`), verified by `go test ./...`, `npm test`, and the Chromium E2E suite (`web/e2e/browser.e2e.mjs`).

---

## 1. Security Perimeter & Defensive Architecture

The system treats telemetry, session identifiers, and cost metrics as strictly confidential data. The defensive architecture follows defense-in-depth principles: zero trust in client-controlled routing headers, strict origin and framing isolation, and immediate fail-closed rejection of path traversal and scanner probes.

### 1.1 Path Traversal & URL Sanitization

All incoming request URIs must pass perimeter filtering before routing or filesystem resolution:

1. **Traversal Sequence Rejection**:
   - The server must immediately reject any URI containing dot-dot traversal sequences, whether literal or encoded:
     - Literal: `..`
     - URL-encoded: `%2e%2e`, `.%2e`, `%2e.`, `%252e%252e` (double encoding)
     - Backslash variants: `\`, `%5c`, `..%5c`, `%2e%2e%5c`
     - Null bytes: `\0`, `%00`
   - Rejection status: **HTTP 404 Not Found** (indistinguishable from non-existent resources to avoid providing oracle information to fuzzers) or **HTTP 400 Bad Request** if control characters are present.
2. **Multiple Leading Slash Sanitization**:
   - Consecutive leading slashes (`//`, `///`) must be collapsed into a single leading slash (`/`) prior to path parsing or resolution to prevent protocol-relative bypasses.
3. **Scanner Probe Defense**:
   - Paths associated with automated scanners, bot crawlers, or vulnerability probes must return an instantaneous **HTTP 404** without executing route handlers:
     - `/.env`, `/.env.local`, `/.env/*`
     - `/.git`, `/.git/*`, `/.svn/*`, `/.hg/*`, `/.ds_store`
     - `/wp-admin/*`, `/wp-login.php*`, `/wp-content/*`, `/xmlrpc.php*`
     - `/phpmyadmin/*`, `/server-status*`, `/server-info*`, `/actuator/*`
     - `/.well-known/security.txt/*`
4. **URL Length & Character Bounds**:
   - Any raw URI exceeding 2048 characters or containing unescaped control characters (`[\x00-\x1f]`) must be rejected with **HTTP 400 Bad Request**.

### 1.2 Reverse Proxy Spoofing & Header Smuggling Denial

The backend listens exclusively on loopback (`127.0.0.1` / `::1`) and enforces a zero-trust policy against client-forged proxy headers:

1. **Backend Isolation (Direct Loopback)**:
   - When running as the backend, any request bearing forwarding headers must be immediately rejected with **HTTP 403 Forbidden**:
     - `X-Forwarded-For`
     - `X-Forwarded-Host`
     - `X-Forwarded-Proto`
     - `X-Forwarded-Port`
     - `X-Real-IP`
     - `Forwarded`
     - `Forwarded-*`
     - `Via`
2. **Gateway / Edge Proxy Validation**:
   - At the edge, only the single trusted reverse proxy may assert forwarding parameters.
   - Evidence of proxy chaining (multiple hops in `X-Forwarded-For`, presence of `Via` or `Forwarded`) must result in **HTTP 403 Forbidden**.
   - Host and protocol assertions (`X-Forwarded-Host`, `X-Forwarded-Proto`, and `X-Forwarded-Port`) must match configured expectations. If an explicit non-standard port is configured (e.g. `8443`), requests presenting mismatched ports (e.g. `443`) must be rejected (HTTP 421 or 403).
3. **Duplicate Header Smuggling Prevention**:
   - The server must inspect all header occurrences (including multi-value headers or raw headers). If duplicate headers contain conflicting or blacklisted values (such as automated bot user agents or multi-hop forwarding IPs), the request must be rejected.
4. **Rate Limiting**:
   - Global rate limiter (default: 600 requests per minute) blunts automated volumetric scraping regardless of claimed source IP.

### 1.3 Strict Origin & CSRF Policy

Cross-Site Request Forgery and unauthorized cross-origin data extraction are prevented through deterministic origin verification:

1. **Origin Verification Matrix**:
   - **Origin Header Present**: Any request bearing an `Origin` header (including `GET` and `HEAD` requests) **must** validate against the allowed origins list. Unrecognized origins receive **HTTP 403 Forbidden**.
   - **Mutating Requests**: All state-changing verbs (`POST`, `PUT`, `DELETE`, `PATCH`) **require** a valid `Origin` header matching the allowlist AND `Content-Type: application/json`.
2. **Allowed Origins Construction**:
   - Primary configured origin (e.g., `https://usage.example.com`).
   - Loopback aliases: When configured with loopback HTTP (`http://localhost:PORT`), the server must automatically allow `http://127.0.0.1:PORT` and `http://[::1]:PORT`.
   - Wildcard matching: Explicit `allowedOrigins` entries supporting single-label wildcards (e.g., `https://*.example.dev` matches `https://sub.example.dev`, but not `https://a.b.example.dev` or `https://example.dev`).
3. **External Document Navigation Exception**:
   - Cross-site requests (`sec-fetch-site: cross-site`) are blocked with **HTTP 403**, with exactly one exception: `GET` requests to entry paths (`/` or `/access`) with `sec-fetch-mode: navigate` and `sec-fetch-dest: document`.

### 1.4 Framing Prevention & Security Headers

1. **Framing Rejection**:
   - Any request with `sec-fetch-dest` set to `iframe`, `frame`, `embed`, or `object` must immediately fail closed with **HTTP 403 Forbidden**.
2. **Mandatory HTTP Response Headers**:
   All HTTP responses must enforce the following baseline security headers:
   ```http
   X-Frame-Options: DENY
   Content-Security-Policy: default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; font-src 'self'; object-src 'none'; manifest-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'
   X-Content-Type-Options: nosniff
   Referrer-Policy: no-referrer
   Cache-Control: no-store
   Permissions-Policy: camera=(), microphone=(), geolocation=(), usb=(), payment=(), interest-cohort=()
   ```

### 1.5 Safe Filesystem & Asset Confinement

1. **Icon URL Sanitization (`sanitizeIconUrl`)**:
   - Local paths must be strictly confined to `/icons/` (e.g. `/icons/codex.svg`). Traversal evasions (`/icons/../secret.png`, backslashes `\`, encoded `%2e%2e`, `%5c`) must be rejected.
   - Remote icons must use HTTPS without user credentials.
   - Raster data URIs are permitted for `data:image/(png|jpeg|webp|gif);base64,...`. SVG data URIs are strictly prohibited to prevent script execution.
2. **Non-Interfering Filesystem Access**:
   - Readers must use non-blocking flags (`O_RDONLY | O_NONBLOCK | O_NOFOLLOW`).
   - Never follow symbolic links; never block on FIFOs or named pipes (`lstatSync` / `os.Lstat` file verification).
   - Skip ignored directories: `.git`, `node_modules`, `worktrees`, `dist`, `build`, `scratch`, `tmp`, `venv`, `__pycache__`.
   - Bounded directory traversal depth: maximum depth of 5 levels (`MAX_DEPTH = 5`).

---

## 2. Exact Decimal Pricing & Catalog Invariants

Financial calculations in AI Usage Monitor must maintain zero floating-point inaccuracy. No IEEE 754 64-bit float conversions may occur anywhere in the pricing pipeline.

### 2.1 Arbitrary-Precision Arithmetic

1. **Exact Representation**:
   - All token rates in the catalog and dynamic registrations must be stored as exact decimal strings (e.g. `"1.25"`, `"0.125"`, `"1.5625"`, `"10"`).
   - In Go, all monetary calculations must use an arbitrary-precision decimal library (e.g., `github.com/shopspring/decimal`).
2. **Observed-Model Exposure**:
   - The built-in catalog is an internal calculation source. It must never be published as a fixed model list.
   - Every web/API surface that lists models (including `/api/pricing`) derives them from events observed in telemetry.
   - Unobserved catalog entries must not be exposed; rates resolve through `pricing.LookupRate`, where rates registered by ingestion take precedence.
3. **Unit Conversion**:
   - Token rates represent USD cost per **1,000,000 (1M) tokens**.
   - Formula:
     $$\text{Cost} = \frac{\text{Tokens} \times \text{Rate} \times \text{ContextScale}}{1{,}000{,}000}$$

### 2.2 Pricing Breakdown Formulas

Given:
- $T_{\text{in}}$: Total input tokens
- $T_{\text{cached}}$: Cached input tokens (cache read)
- $T_{\text{cwrite}}$: Cache write input tokens
- $T_{\text{out}}$: Total output tokens
- $T_{\text{reasoning}}$: Reasoning output tokens
- $R_{\text{in}}$: Normal input rate per 1M tokens
- $R_{\text{cached}}$: Cache read rate (falls back to $R_{\text{in}}$ if omitted)
- $R_{\text{cwrite}}$: Cache write rate (selected based on duration)
- $R_{\text{out}}$: Output rate per 1M tokens
- $S_{\text{in}}, S_{\text{out}}$: Long context multiplier scales ($1.0$ if under threshold)

Calculations:
1. **Normal Input Cost**:
   $$\text{Cost}_{\text{normal}} = \frac{(T_{\text{in}} - T_{\text{cached}} - T_{\text{cwrite}}) \times R_{\text{in}} \times S_{\text{in}}}{1{,}000{,}000}$$
2. **Cache Read Cost**:
   $$\text{Cost}_{\text{cache\_read}} = \frac{T_{\text{cached}} \times R_{\text{cached}} \times S_{\text{in}}}{1{,}000{,}000}$$
3. **Cache Write Cost**:
   $$\text{Cost}_{\text{cache\_write}} = \frac{T_{\text{cwrite}} \times R_{\text{cwrite}} \times S_{\text{in}}}{1{,}000{,}000}$$
4. **Total Cache Cost**:
   $$\text{Cost}_{\text{cache}} = \text{Cost}_{\text{cache\_read}} + \text{Cost}_{\text{cache\_write}}$$
5. **Output Cost**:
   $$\text{Cost}_{\text{output}} = \frac{T_{\text{out}} \times R_{\text{out}} \times S_{\text{out}}}{1{,}000{,}000}$$
6. **Reasoning Cost** (if reasoning tokens reported):
   $$\text{Cost}_{\text{reasoning}} = \frac{T_{\text{reasoning}} \times R_{\text{out}} \times S_{\text{out}}}{1{,}000{,}000}$$
7. **Visible Output Cost**:
   $$\text{Cost}_{\text{visible}} = \text{Cost}_{\text{output}} - \text{Cost}_{\text{reasoning}}$$
8. **Total Cost**:
   $$\text{Cost}_{\text{total}} = \text{Cost}_{\text{normal}} + \text{Cost}_{\text{cache}} + \text{Cost}_{\text{output}}$$

### 2.3 Fail-Closed Pricing Rules

When rate conditions are ambiguous or unverified, the pricing engine must **fail closed** and return an unestimated status (`costUsd: null` with an explanatory `costNote`), rather than guessing or silently misrepresenting costs:

| Failure Trigger | Condition | Resulting Cost Note |
|---|---|---|
| **Unknown Model / Provider Mismatch** | Model not in catalog/dynamic tables, or declared provider does not match catalog provider (after canonical normalization). | `"Tarifa no verificada"` (`NoteUnknownRate`) |
| **Deprecated / Retired Model** | Model is retired in the internal catalog and no agent-supplied rate overrides it. | `"Modelo retirado sin tarifa activa verificada"` (`NoteDeprecatedRate`) |
| **Incomplete Cache Breakdown** | Event provides total input tokens but `cachedInputTokens` or `cacheWriteInputTokens` is `null`. | `"Desglose de caché incompleto"` (`NoteIncompleteCache`) |
| **Modality Unverified** | Catalog specifies a specific modality (e.g. `"text"` on `gemini-2.5-flash-lite`), but event modality is unspecified or mismatched. | `"Modalidad de entrada no verificada: la tarifa difiere por modalidad"` (`NoteModalityUnverified`) |
| **Missing Cache-Write Rate** | Event has $T_{\text{cwrite}} > 0$, but model pricing lacks `cacheWrite` rate. | `"Escritura/almacenamiento de caché sin tarifa verificada"` (`NoteCacheWriteUnavailable`) |
| **Missing 12h Cache-Write Tier** | Event explicitly specifies `cacheWriteDuration: "12h"`, $T_{\text{cwrite}} > 0$, but model lacks `cacheWrite12h`. | `"Escritura/almacenamiento de caché sin tarifa verificada"` (`NoteCacheWriteUnavailable`) |

### 2.4 Hidden Pure-Go Tokenization & Reconciliation Isolation

1. **Silent Fallback Only**: Multi-family pure-Go tokenizers (`internal/tokenizer`, covering OpenAI, Anthropic/Claude, Google/Gemini, Mistral Tekken & SentencePiece, MiniMax, Z.AI GLM, DeepSeek, Qwen, Kimi, and Llama/Grok) execute exclusively inside local ingestion when a source emits visible text without native token counters.
2. **Strict Separation**: Fallback text measurements are stored in `text_measurements` for reconciliation only, are never inserted into `events`, are never summed into native KPIs or costs, and are removed automatically if native counters arrive for the same event.
3. **Zero Public Surface**: Neither the web UI nor public API endpoints (`/api/sources`, `/api/reconciliation`, etc.) may expose tokenizer tabs, selectors, engine names, or vocabulary metadata.

> [!CRITICAL]
> **12h Cache-Write Fail-Closed Guarantee**: Under no circumstances may a declared `12h` cache write duration fall back to the `30m` standard rate. If a model does not declare an explicit `cacheWrite12h` rate, any 12h cache write event must return unestimated.

---

## 3. Ingestion Robustness & Data Integrity

The event collector and source adapters must ingest telemetry passively from external agent-owned processes without corrupting cursors, dropping events, or interfering with file locks.

### 3.1 Same-Size Log Rewrite Detection

On coarse-clock filesystems (or high-throughput containerized environments), an agent file rewrite can occur within the same millisecond or with the exact same byte length, leaving `inode`, `size`, and filesystem timestamps (`mtime`/`ctime`) unchanged.

1. **Tail Content Sampling**:
   - In addition to standard `stat` metadata (`st.ino`, `st.size`, `st.mtimeMs`), the collector maintains an in-memory bounded tail sample hash:
     - Sample size: `CONTENT_SAMPLE_BYTES = 4096`.
     - Read the last $\min(4096, \text{size})$ bytes of the file.
     - Compute the SHA-256 hash: `tailSignature(fd, size)`.
2. **Cursor Reset Invariant**:
   - If `stat` metadata matches the previously recorded signature, but the tail content sample differs:
     - The collector detects a same-size rewrite.
     - The read cursor must reset to offset `0`.
     - Persisted cursor schema (128-byte signature at previous offset) remains protected.

### 3.2 Canonical Identifier Representation

- SQLite 64-bit integer row IDs and large JSON identifiers (e.g. `9007199254740993123456789`) exceed JavaScript `Number.MAX_SAFE_INTEGER`.
- All record identifiers, response IDs, and session IDs must be converted to **canonical string format** (`canonicalId(v) = String(v)`) before equality checks, indexing, or persistence. No conversion to floating-point numbers or truncated integers is permitted.

### 3.3 Blank-Line Byte Offset Tracking

- When parsing JSONL log files, empty or whitespace-only lines carry no valid record.
- However, each blank line's newline character (`\n`) consumes a byte.
- The parser's byte offset counter must increment across blank lines:
  $$\text{byteOffset} \mathrel{+}= \text{Buffer.byteLength}(\text{line}) + 1$$
- Skipping this increment causes all subsequent error locations and cursor resume offsets to drift.

### 3.4 Pre-Validation of Epoch-MS Timestamps

- External agents may report timestamps formatted as numeric epoch milliseconds (e.g. `1730000000000`) instead of ISO-8601 strings.
- The collector must resolve timestamps according to the agent's declared `occurredAtFormat` (converting `epoch-ms` integers to valid Date/time structures) **before** validating whether the timestamp is finite and meets activation boundaries.

### 3.5 Specific Error Classification

Errors encountered during log normalization must be classified with specific machine-readable kinds rather than generic failures:
1. **Missing Metrics**: If required token counters (`inputTokens`, `outputTokens`) are missing, emit `<agent_id>_missing_metric` (not `invalid_usage`).
2. **Silent Drop Prevention**: If a catalog query returns null and fallback content extraction yields no usable text, record `<agent_id>_no_usage` in the errors table rather than silently dropping the record.
3. **Breakdown Bounds**: Enforce:
   - $T_{\text{cached}} + T_{\text{cwrite}} \le T_{\text{in}}$
   - $T_{\text{reasoning}} \le T_{\text{out}}$
   - Violations must throw `invalid_breakdown`.

---

## 4. Frontend UX, Typography & Accessibility Standards

The user interface represents a technical observability console and must deliver pixel-perfect clarity, zero visual jitter, and strict accessibility compliance.

### 4.1 WCAG AA Contrast Compliance

Contrast ratios must meet or exceed WCAG AA requirements (minimum 4.5:1 for normal text, 3:1 for large text / UI elements) across all 4 themes in both **Dark** and **Light** modes:

1. **Theme Inventory**:
   - `bioluminescent` (Obsidiana): Neutral dark obsidian canvas (`#0a0a0c`), emerald primary (`#10b981`), sky blue secondary (`#60a5fa`). Light mode uses crisp arctic surfaces with `#616876` muted text.
   - `cute-pastel-blue` (Zafiro): Powder blue / starlight blue. Light mode muted text remediated to `#596a82`.
   - `burned-paper` (Ámbar): Raw kraft cardboard / smoldering amber. Light mode muted text remediated to `#6e6055`, primary accent `#b45309`.
   - `vivid-yellow-green` (Esmeralda): Meadow emerald / sun yellow. Light mode muted text remediated to `#5b6e5e`, primary accent `#16a34a`.
2. **Contrast Invariants**:
   - `--text-primary` on background canvas $\ge 7:1$.
   - `--text-secondary` on panel surfaces $\ge 4.5:1$.
   - `--text-muted` on canvas/panels $\ge 4.5:1$.

### 4.2 Tabular Numerics & Zero Metric Jitter

Live streaming telemetry updates numbers dynamically:
1. **Tabular Numerics**:
   - All numeric counters, token statistics, currency amounts, percentages, and table data cells must declare:
     ```css
     font-variant-numeric: tabular-nums;
     font-feature-settings: 'tnum' 1;
     ```
2. **Kerning & Synthesis Stabilization**:
   - `:root` declares `font-kerning: none` and `font-synthesis: none` to prevent layout reflow during live metric ticks.

### 4.3 Humanist Typography Stack

- **Font Family**: Self-hosted variable Inter (`--font-sans: Inter, -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;`).
- **Body Rhythm**: Relaxed line-height `1.65` for optimal technical legibility.
- **Monospace Stack**: `--font-mono: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;`.

### 4.4 Tactile Micro-Interactions & Reduced Motion

1. **Deterministic Tactile Curve**:
   - Micro-interactions use `--ease-tactile: cubic-bezier(0.2, 0, 0, 1)`.
   - Overshoot spring curves and decorative bouncing are strictly forbidden.
2. **Deflection States**:
   - Hover: `translateY(-1px)` with surface brightening and `--lift-shadow`.
   - Active / Press: Immediate downward deflection `translateY(1px) scale(0.99)` with duration `0.08s` (`--press-duration`).
3. **High-Contrast Focus Outlines**:
   - `:focus-visible` must display `2px solid var(--border-focus); outline-offset: 2px;`.
4. **Prefers-Reduced-Motion Guarantees**:
   - When `@media (prefers-reduced-motion: reduce)` is active:
     - Tab crossfades and disclosures must execute instantaneously (`transition-duration: 0s`).
     - Ambient breathing, floating, and light sweep keyframes must halt (`animation: none`).
     - Element opacity must remain `1` (no stuck fade-ins).
     - Focus outlines must remain fully visible and non-zero.
