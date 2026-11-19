export function formatCompact(raw: string | number | null | undefined): string {
  const n = Number(raw ?? 0);
  if (!Number.isFinite(n)) return String(raw ?? '0');
  return new Intl.NumberFormat('es-ES', {
    notation: 'compact',
    maximumFractionDigits: 1,
  }).format(n);
}

export function formatExact(raw: string | number | null | undefined): string {
  if (raw == null || raw === '') return '0';
  try {
    const s = String(raw).trim();
    if (/^\d+$/.test(s)) {
      return BigInt(s).toLocaleString('es-ES');
    }
    const n = Math.round(Number(s));
    return Number.isNaN(n) ? s : BigInt(n).toLocaleString('es-ES');
  } catch {
    return String(raw);
  }
}

export function formatMoney(raw: string | number | null | undefined, precise = false): string {
  if (raw == null || raw === '') return '—';
  const n = Number(raw);
  if (!Number.isFinite(n)) return '—';
  const maxFrac = precise ? 6 : n > 0 && n < 0.01 ? 4 : 2;
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
    maximumFractionDigits: maxFrac,
  }).format(n);
}

export function formatShortTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleTimeString('es-ES', { hour: '2-digit', minute: '2-digit' });
}

export function formatFullTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString('es-ES');
}

export function pctRatio(part: string | undefined, total: string | undefined): number {
  const p = Number(part ?? 0);
  const t = Number(total ?? 0);
  if (!Number.isFinite(p) || !Number.isFinite(t) || t <= 0) return 0;
  return Math.min(100, Math.max(0, (p / t) * 100));
}
