# Design System: Bioluminescent Observability Console

Design specification for `ai-usage-monitor` (Go + Svelte 5 single-binary console).

## 1. Visual Theme & Atmosphere

- **Theme & Baseline**: Strict neutral-dark telemetry console by default (`html[data-theme='bioluminescent'][data-mode='dark']`, UI label `Obsidiana`, `color-scheme: dark`). The base canvas is deep obsidian (`#0a0a0c`) layered with restrained neutral dark surfaces (`#0f0f12`, `#131317`, `#1a1a20`, `#22222a`), without ambient radial background gradients or tinted textures.
- **Physical Metaphor**: Clean, restrained technical telemetry console. High-density data observability with live inline status dots (`.live-dot-inline`), SVG bar/area charts and sparklines, jitter-free tabular counters (`font-kerning: none` and `tabular-nums`), and clear dual-accent separation (Emerald `#10b981` for `--accent-primary`, input series, cache ratio, and cost highlights; Sky Blue `#60a5fa` for `--accent-secondary`, output series, reasoning ratio, and cumulative cost).
- **Surface Elevation & Layering**:
  - _Root Canvas_: `#0a0a0c` (`--bg-canvas`) for the main workspace and sticky header.
  - _Persistent Sidebar_: `#0f0f12` (`--bg-sidebar`) separated by a crisp 1px structural hairline (`#23232b`, `--border-primary`).
  - _Metric Panels & Cards_: Flat layered surface `#131317` (`--bg-panel`) with `1px solid #23232b` (`--border-primary`) across `.panel`, `.stat`, `.telemetry-kpi-item`, `.agent-card`, `.agent-status`, `.segments`, `.mobile-tabs`, and `.theme-dropdown-menu`.
  - _Interactive Tiles & Controls_: `#1a1a20` (`--bg-tile`) for resting buttons, active navigation items, selected segment pills, `.detected-card`, `.event-detail`, and bar tracks; lifting to `#22222a` (`--bg-tile-hover`) on hover.
  - _Sticky Top Header_: `54px` (`50px` at `<= 900px`) on `#0a0a0c` (`--bg-canvas`) with a 1px bottom hairline (`#23232b`, `--border-primary`).
  - _Mobile Off-Canvas Backdrop_: `rgba(0, 0, 0, 0.55)` behind the drawer (`box-shadow: 8px 0 32px rgba(0, 0, 0, 0.55)`).
- **Theme Scope**: This specification documents the default `bioluminescent` dark theme (`Obsidiana`). Three additional themes (`cute-pastel-blue` / `Zafiro`, `burned-paper` / `Ámbar`, `vivid-yellow-green` / `Esmeralda`) and `light` mode variants for all four themes live in `web/src/app.css` (with `<meta name="theme-color">` and anti-FOUC initialization in `web/src/lib/theme.svelte.ts` and `web/public/theme-init.js`).
- **CSP Hygiene**: Zero inline `style` attributes or inline scripts anywhere in the DOM (`style-src 'self'; script-src 'self'`). All layout, chart, and theme styling lives in CSS stylesheets and SVG presentation attributes.

---

## 2. Color Palette & Roles

### Primary Foundation & Surfaces (`:root` / `bioluminescent` dark)

- **Obsidian Root (`--bg-canvas: #0a0a0c`)**: Deepest background canvas for the application shell, sticky `<header>`, and `/access` (`AccessView.svelte`).
- **Sidebar Surface (`--bg-sidebar: #0f0f12`)**: Persistent navigation drawer (`aside`) and scrollbar track (`--scrollbar-track: #0f0f12`).
- **Panel Base (`--bg-panel: #131317`)**: Standard background for `.panel`, `.stat`, `.telemetry-kpi-item`, `.agent-card`, `.agent-status`, `.detected-chip`, `.segments`, `.mobile-tabs`, `.theme-dropdown-menu`, `.notice`, and `.access-card`.
- **Elevated Tile Surface (`--bg-tile: #1a1a20`)**: Background for standard buttons, active navigation and tab pills, `.detected-card`, `.mini-progress-bar` tracks, `.chip-btn`, `.count` badges, and `.event-detail`.
- **Interactive Hover Surface (`--bg-tile-hover: #22222a`)**: Hover background for standard buttons, `.icon-button`, unselected `.segments` buttons, and `.models-disclosure-summary`.
- **Scrollbars & Depth**: `--scrollbar-thumb: #2a2a34`, `--scrollbar-thumb-hover: #3a3a47`, `--scrollbar-track: #0f0f12`, `--scroll-shadow: rgba(0, 0, 0, 0.35)`.

### Borders & Structural Hairlines

- **Primary Structural Border (`--border-primary: #23232b`)**: Canonical 1px perimeter for `.panel`, `.stat`, `.agent-card`, `aside`, `header`, buttons, `.segments`, `th` bottom divider, and `.theme-dropdown-menu`.
- **Secondary Divider Hairline (`--border-subtle: #1c1c23`)**: Table row borders (`td`), `.models-disclosure` separators, `.history-item` dividers, and `.event-detail` top hairline.
- **Focus & Interactive Highlight Border (`--border-focus: #10b981`)**: `:focus-visible` outline (`2px solid var(--border-focus); outline-offset: 2px;`) and hover border accent on standard buttons.

### Interactive, Accent & Chart Colors

- **Emerald Primary (`--accent-primary: #10b981`)**: Primary brand and telemetry accent:
  - `--accent-primary-text: #04130d` (ink on primary accent fills and `/access` CTA button).
  - `--accent-primary-bg: rgba(16, 185, 129, 0.12)` (`.chip-btn.active`, `.models-count-badge`, and `/access` `.brand-mark`).
  - `--chart-series-1: #10b981` (input token bars and sparkline input series).
  - `--status-live-dot: #10b981` (live inline header dot and active agent dots).
  - Applied to `.brand svg`, `.stat.cost strong`, `.mint-text`, `.mint-fill`, `.status-inline.active`, `.icon-button:hover svg`, and expanded `.models-chevron`.
- **Sky Blue Secondary (`--accent-secondary: #60a5fa`)**: Secondary analytical accent:
  - `--accent-secondary-bg: rgba(96, 165, 250, 0.12)`.
  - `--chart-series-2: #60a5fa` (output token bars, sparkline output series, and cumulative cost area/polyline).
  - Applied to `.violet-text` and `.violet-fill` (reasoning telemetry readout and alternating detected-model bars).
- **Chart Grid & Axis Ink**: `--chart-grid: #1f1f27`, `--chart-text: #747782`.
- **Auth Validation Error**: `--danger: #f87171` (dark) / `#dc2626` (light).

### Typography Hierarchy

- **Primary Ink (`--text-primary: #f4f4f6`)**: Headings, hero KPIs (`.stat > strong`), active nav/tab labels, `.brand`, `.top-title`, `.agent-badge`, and primary card values.
- **Secondary Ink (`--text-secondary: #b4b6be`)**: Navigation buttons (`nav button`), table cells (`td`), `.icon-button`, `.chart-readout`, `.event-detail-item dd`, and `.notice` text.
- **Muted Tertiary / Metadata (`--text-muted: #7b7e89`)**: Section labels (`.nav-label`), `small` / `.subtle`, table headers (`th`), `.stat > span`, `.kpi-label`, inactive pills, `.pagination`, and `.empty` states.
- **Faint Hairline Ink (`--text-faint: #52555f`)**: Default inactive `.state-dot` and `.status-inline i` indicators.

### Dynamic Multi-Agent Identity Tones

Defined globally in `web/src/app.css` (with darkened `--agent-color` overrides under `html[data-mode='light']`):

- **Mint (`.agent-mint`)**: `--agent-color: #10b981; --agent-bg: rgba(16, 185, 129, 0.12); --agent-border: rgba(16, 185, 129, 0.24);`
- **Blue (`.agent-blue`)**: `--agent-color: #60a5fa; --agent-bg: rgba(96, 165, 250, 0.12); --agent-border: rgba(96, 165, 250, 0.24);`
- **Violet (`.agent-violet`)**: `--agent-color: #a78bfa; --agent-bg: rgba(167, 139, 250, 0.12); --agent-border: rgba(167, 139, 250, 0.24);`
- **Amber (`.agent-amber`)**: `--agent-color: #f59e0b; --agent-bg: rgba(245, 158, 11, 0.12); --agent-border: rgba(245, 158, 11, 0.24);`
- **Rose (`.agent-rose`)**: `--agent-color: #fb7185; --agent-bg: rgba(251, 113, 133, 0.12); --agent-border: rgba(251, 113, 133, 0.24);`
- **Cyan (`.agent-cyan`)**: `--agent-color: #22d3ee; --agent-bg: rgba(34, 211, 238, 0.12); --agent-border: rgba(34, 211, 238, 0.24);`

### Status & Telemetry States

- **Live / Running (`.live-dot-inline.on`, `.state-dot.state-running`, `.agent-source-state.state-running i`, `.status-inline.active`)**: Dot filled with `--status-live-dot` (`#10b981`); `.status-inline.active` label uses `--accent-primary` (`#10b981`).
- **Offline / Idle / Waiting**: `.live-dot-inline` and `.agent-source-state i` default to `--text-muted` (`#7b7e89`); `.state-dot` and `.status-inline i` default to `--text-faint` (`#52555f`).
- **Status Notice / Auth Error**: `.notice` uses `var(--bg-panel)` with `1px solid var(--border-primary)` and `var(--text-secondary)`; access validation errors use `--danger`.

---

## 3. Typography Rules

- **Primary & Monospace Typefaces**: Self-hosted variable `Inter` (`/fonts/inter-latin.woff2`, weights `100 900`, `font-display: swap`, SIL OFL 1.1; see `NOTICE.md`) declared via `--font-sans: Inter, -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;`, paired with `--font-mono: ui-monospace, SFMono-Regular, 'SF Mono', Menlo, Monaco, Consolas, monospace;`.
- **Numeric Telemetry**: `font-kerning: none` and `font-synthesis: none` on `:root` eliminate pairwise kerning reflow during live updates. `font-variant-numeric: tabular-nums` and `font-feature-settings: 'tnum' 1` apply across `.stat > strong`, `td`, `svg text`, `.models-count-badge`, `.segments button`, `.kpi-num`, `.chart-readout`, `.model-bar-val`, `.agent-card-metrics strong`, `.count`, `.event-detail-item dd`, and `.pagination`.
- **Type Scale**:
  - _Auth Heading (`.access-card h1`)_: `20px`, weight `600`, tracking `-0.02em`.
  - _Top Header Title (`.top-title`)_: `16px`, weight `600`, tracking `-0.02em` (main page `h1` uses `.sr-only`).
  - _Brand Wordmark (`.brand`)_: `15px`, weight `600`, tracking `-0.02em`.
  - _Panel Headings (`h2` / `h3`)_: `14px`, weight `600`, tracking `-0.01em` (`h2`).
  - _Section Label (`.nav-label`)_: `11px`, weight `600`, color `var(--text-muted)`.
  - _Primary Metric Displays (`.stat > strong`)_: `26px` (`23px` at `<= 640px`), weight `600`, line-height `1.1`, tracking `-0.03em`.
  - _Secondary Card KPIs_: `.agent-card-metrics strong` `18px` (weight `600`); `.kpi-num` `14px` (weight `600`).
  - _Body Copy (`body`)_: `14px`, weight `400`, line-height `1.65`, color `var(--text-primary)`.
  - _Navigation, Tables & Controls_: `13.5px` (`nav button`, `.models-disclosure-summary`, `.detected-card-body strong`), `13px` (`button`, `table`, `.theme-menu-item`, `.notice`), `12.5px` (`.segments button`, `.mobile-tabs button`, `.event-detail-item dd`).
  - _Headers, Labels & Microcopy_: `12px` (`.stat > span`, `.kpi-label`, `.legend`, `.chart-readout`, `.chip-btn`, `.pagination`), `11.5px` (`th`, `td small`, `.status-inline`, `.models-count-badge`), `11px` (`.agent-card-metrics small`, `.event-detail-item dt`), `10px` (chart SVG `<text>`).

---

## 4. Component Stylings

### Buttons & Interactive Controls

- **Standard Button (`button`)**: `var(--bg-tile)` fill, `1px solid var(--border-primary)`, `8px` radius, `7px 12px` padding, `13px`, weight `500`. Hover (`:not(:disabled):not(:active)`): `var(--bg-tile-hover)`, `border-color: var(--border-focus)`, `translateY(-1px)`, `var(--lift-shadow)`. Active (`:not(:disabled)`): `translateY(1px) scale(0.99)`.
- **Primary Auth CTA Button (`.access-submit`)**: `var(--accent-primary)` (`#10b981`) fill, `var(--accent-primary-text)` (`#04130d`) text, `8px` radius, `11px 16px` padding, `14px` font-weight `600`.
- **Navigation Drawer Buttons (`nav button`)**: Transparent fill, `var(--text-secondary)` text, `8px` radius, `8px 10px` padding, `13.5px`. Hover: `var(--bg-tile)`, `var(--text-primary)`, `translateY(-1px)`. Active: `var(--bg-tile)` fill, `var(--text-primary)` text, weight `600`.
- **Segmented Time Window Controls (`.segments`)**: `var(--bg-panel)` container, `1px solid var(--border-primary)`, `9px` radius, `3px` padding, `2px` gap. Pill (`button`): `6px` radius, `5px 14px` padding, `12.5px`, `var(--text-muted)`. Selected pill: `var(--bg-tile)` fill, `var(--text-primary)` text, weight `600`.

### Cards & Metric Panels

- **Standard Container Panel (`.panel`)**: `var(--bg-panel)` background, `1px solid var(--border-primary)`, `12px` radius, `overflow: hidden`, `margin-bottom: 14px`. Header padding `14px 16px 10px`.
- **Telemetry Metric Tile (`.stat`)**: `var(--bg-panel)` background, `1px solid var(--border-primary)`, `10px` radius, `14px 16px` padding. Cost figure (`.stat.cost strong`) highlighted in `var(--accent-primary)`.
- **Agent Dashboard Card (`.agent-card`)**: `var(--bg-panel)` background, `1px solid var(--border-primary)`, `12px` radius, `14px 16px` padding. Hover: `border-color: var(--agent-border, var(--border-focus))`.
- **Detected Models Disclosure (`details.models-disclosure`)**: Collapsible `.panel` with `.models-disclosure-summary` (`12px 16px` padding, `.models-count-badge` pill in `var(--accent-primary-bg)` / `var(--accent-primary)`, rotating `.models-chevron`) and `.models-disclosure-body`.

### Efficiency Strip, Progress Bars & Sparklines

- **Efficiency KPI Strip (`.telemetry-bar-kpis` / `.telemetry-kpi-item`)**: Two-column grid of `var(--bg-panel)` tiles (`1px solid var(--border-primary)`, `10px` radius, `12px 14px` padding) displaying Cache (`.mint-text` / `.mint-fill`, `--accent-primary`) and Reasoning (`.violet-text` / `.violet-fill`, `--accent-secondary`).
- **Progress Bars (`.mini-progress-bar`)**: `4px` height, `var(--bg-tile)` track, `99px` radius, `transition: width 0.25s ease`.
- **Agent Sparklines (`.agent-sparkline`)**: `40px` height SVG (`viewBox="0 0 300 40"`) with `--chart-grid` baseline and `1.5px` `--chart-series-1` / `--chart-series-2` polylines.

### Badges, Chips & Avatars

- **Inline Live Indicator (`.live-dot-inline`)**: `7px` circular dot in header (`--text-muted` idle, `--status-live-dot` when `.on`).
- **Agent Avatar Mark (`.agent-mark`)**: `28px x 28px` (`34px x 34px` in `.agent-status`), `7px` radius, `var(--agent-bg, var(--bg-tile))` fill, `1px solid var(--agent-border, var(--border-primary))`, `var(--agent-color, var(--accent-primary))` ink.
- **Detected Agent Chip (`.detected-chip`)**: `var(--bg-panel)` fill, `1px solid var(--border-primary)`, `10px` radius, `7px 12px 7px 8px` padding, with `6px` `.state-dot`.

### Data Tables & Accessible History Disclosures

- **Header (`th`)**: Transparent background inside `.table-scroll` (over `--bg-panel`), `1px solid var(--border-primary)` bottom border, `10px 16px` padding, `var(--text-muted)`, `11.5px` weight `500`.
- **Row Cells (`td`)**: `1px solid var(--border-subtle)` bottom border, `11px 16px` padding, `var(--text-secondary)`, `tabular-nums` + `'tnum' 1`.
- **Expandable History List (`ul.history-list` / `li.history-item` / `details.event-disclosure`)**: Semantic unordered list (avoiding invalid `role="table"` wrappers over `<details>`). Visual column headers sit above the list with `aria-hidden="true"`, while each `li.history-item` wraps a native `<details class="event-disclosure">` with a 4-column `<summary class="event-summary">` (`58px minmax(0, 1fr) 98px 92px`, `50px minmax(0, 1fr) 84px 80px` at `<= 640px`), `.event-chevron` rotating `90deg` when `[open]`, and `.event-detail` (`var(--bg-tile)` background, `1px solid var(--border-subtle)` top hairline).

---

## 5. Layout Principles

- **Viewport & Container**: `main { max-width: 1180px; margin: 0 auto; padding: 18px 20px 36px; }` (`12px 14px 32px` at `<= 900px`).
- **Spatial Grid**: Compact `10px`–`14px` card/grid gaps and `12px`–`20px` container padding.
- **Off-Canvas Navigation**:
  - _Desktop (`> 900px`)_: Fixed `212px` left sidebar (`aside`), collapsible via `.sidebar-collapsed` (`transform: translateX(-100%)`); `.main-wrap` shifts smoothly between `margin-left: 212px` and `0`.
  - _Tablet / Mobile (`<= 900px`)_: Floating modal drawer (`min(260px, 82vw)`) over a `rgba(0, 0, 0, 0.55)` `.sidebar-backdrop`.
- **Three Operational Views**: `Vista general` (`overview`), `Historial de eventos` (`history`), and `Fuentes y precisión` (`sources`). Tokenization runs silently in Go as an internal fallback and has zero tabs, selectors, or controls in the UI.
- **Responsive Breakpoints (`web/src/app.css`)**:
  - `> 900px`: Fixed `212px` sidebar, `54px` sticky header, 4-column `.stats`, 2-column `.agent-cards` / `.detected-grid` / `.telemetry-bar-kpis`.
  - `<= 900px`: Off-canvas drawer, `.mobile-tabs` strip visible, `50px` sticky header, full-width `.segments`.
  - `<= 640px`: 2-column `.stats`, 1-column `.agent-cards` / `.detected-grid` / `.telemetry-bar-kpis`, `.stat > strong` `23px`, compact history grid (`50px minmax(0, 1fr) 84px 80px`).

### Scrolling & Scroll Containment

- **Root Scroll Padding**: `scroll-padding-top: 72px` on `html` / `:root` clears the `54px` sticky header (`50px` at `<= 900px`) with comfortable buffer so keyboard focus and fragment targets never land beneath the header.
- **Overscroll Containment**: `overscroll-behavior: contain` on `.table-scroll` and `.aside-scroll` prevents scroll chaining from table/sidebar to the page.
- **Table Scroll Shadow Cues**: `.table-scroll` uses a 4-layer gradient (`background-attachment: local, local, scroll, scroll`) with local covers (`32px 100%`) wider than scroll cues (`12px 100%`).

---

## 6. Interactive Buttons & Controls System

### 6.1 Visual Philosophy: Compact, Deterministic & Tactile

Interactive controls use crisp, deterministic micro-interactions without overshoot springs (`web/e2e/browser.e2e.mjs` enforces sober motion and zero idle animations):

- **Deterministic Tactile Easing**: `--ease-tactile: cubic-bezier(0.2, 0, 0, 1);`, `--lift-shadow: 0 3px 8px -2px rgba(0, 0, 0, 0.25);`, and `--press-duration: 0.08s;` (`web/src/app.css`).
- **Hover Elevation**: Hover states (`:hover:not(:disabled):not(:active)`) elevate the control (`translateY(-1px)`), brighten surfaces/borders, and apply `var(--lift-shadow)`.
- **Tactile Click/Press Response**: `:active:not(:disabled)` states deflect immediately (`translateY(1px) scale(0.99)` or `scale(0.97)`) at `var(--press-duration)` (`0.08s`).
- **Accessible Focus Rings**: High-contrast focus outlines (`2px solid var(--border-focus); outline-offset: 2px;`).
- **Compact Sizing**: Controls remain `>= 24x24` CSS px.

---

## 7. Logos, Emblems & Image System

### 7.1 Multi-Agent Vector Logos (`web/src/components/AgentIcon.svelte` & `web/src/lib/agents.ts`)

Every supported AI technology maps to a dedicated monochrome `currentColor` SVG silhouette (`viewBox="0 0 24 24"`) and canonical agent identifier:

- `codex` — Codex / OpenAI (6-fold radial spiral)
- `hermes` — Hermes / Nous (planetary sphere glyph)
- `opencode` — OpenCode (monolithic dual-frame glyph)
- `antigravity` — Antigravity (ascending hyperbolic vortex)
- `claude` — Claude / Anthropic (sparkburst rosette)
- `gemini` — Gemini / Google (4-point celestial starburst)
- `cursor` — Cursor (isometric cube)
- `copilot` — Copilot / GitHub (paired loop ribbons)
- `deepseek` — DeepSeek (whale silhouette)
- Generic fallback (rounded terminal icon)

External `iconUrl` values supplied via agent manifests are sanitized on the server (`internal/security`) and rendered at `15px x 15px` with immediate SVG fallback on error.

---

## 8. Motion & Accessibility Standards

- **Tactile Easing Token**: `--ease-tactile: cubic-bezier(0.2, 0, 0, 1);` applied to button/control transforms alongside standard `ease` surface transitions.
- **No Ambient Animations**: No continuous pulse, glow, or floating keyframes.
- **Accessibility (`prefers-reduced-motion: reduce`)**:
  Under `@media (prefers-reduced-motion: reduce)`:
  - Global suppression sets `animation: none !important; transition: none !important; scroll-behavior: auto !important;`.
  - Tactile hover/press transforms on `button`, `nav button`, `.icon-button`, and `.segments button` are neutralized (`transform: none !important;`), leaving structural off-canvas sidebar transforms functional.
  - `.tab-content` stays at `opacity: 1 !important;` and `:focus-visible` retains `2px solid var(--border-focus) !important; outline-offset: 2px !important;`.
