import type { ThemeId, ThemeMode } from './types';

export interface ThemeMeta {
  id: ThemeId;
  label: string;
  darkBg: string;
  lightBg: string;
}

export const THEMES: ThemeMeta[] = [
  { id: 'bioluminescent', label: 'Obsidiana', darkBg: '#0a0a0c', lightBg: '#f6f7f9' },
  { id: 'cute-pastel-blue', label: 'Zafiro', darkBg: '#0c131a', lightBg: '#f3f7fb' },
  { id: 'burned-paper', label: 'Ámbar', darkBg: '#161311', lightBg: '#f5efe6' },
  { id: 'vivid-yellow-green', label: 'Esmeralda', darkBg: '#0c120e', lightBg: '#f8faf6' },
];

const VALID_THEMES = new Set<string>(THEMES.map((t) => t.id));

function readInitialTheme(): ThemeId {
  try {
    const raw = localStorage.getItem('pulse_ui_theme');
    if (raw && VALID_THEMES.has(raw)) return raw as ThemeId;
  } catch {}
  return 'bioluminescent';
}

function readInitialMode(): ThemeMode {
  try {
    const raw = localStorage.getItem('pulse_ui_mode');
    if (raw === 'light' || raw === 'dark') return raw;
  } catch {}
  return 'dark';
}

function applyThemeToDOM(theme: ThemeId, mode: ThemeMode): void {
  if (typeof document === 'undefined') return;
  document.documentElement.setAttribute('data-theme', theme);
  document.documentElement.setAttribute('data-mode', mode);
  const meta = document.querySelector('meta[name="theme-color"]');
  const info = THEMES.find((t) => t.id === theme);
  if (meta && info) {
    meta.setAttribute('content', mode === 'light' ? info.lightBg : info.darkBg);
  }
}

class ThemeStore {
  theme = $state<ThemeId>(readInitialTheme());
  mode = $state<ThemeMode>(readInitialMode());

  setTheme(id: ThemeId): void {
    this.theme = id;
    try {
      localStorage.setItem('pulse_ui_theme', id);
    } catch {}
    applyThemeToDOM(this.theme, this.mode);
  }

  toggleMode(): void {
    this.mode = this.mode === 'dark' ? 'light' : 'dark';
    try {
      localStorage.setItem('pulse_ui_mode', this.mode);
    } catch {}
    applyThemeToDOM(this.theme, this.mode);
  }

  sync(): void {
    applyThemeToDOM(this.theme, this.mode);
  }
}

export const themeStore = new ThemeStore();
