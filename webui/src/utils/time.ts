type DateLike = string | number | Date | null | undefined;

function parseDateLike(value: DateLike): Date | null {
  if (value == null) return null;
  if (value instanceof Date) {
    return Number.isNaN(value.getTime()) ? null : value;
  }
  if (typeof value === 'number') {
    const d = new Date(value);
    return Number.isNaN(d.getTime()) ? null : d;
  }
  const s = String(value).trim();
  if (!s) return null;

  if (/^\d+$/.test(s)) {
    const n = Number(s);
    const ms = n > 1e12 ? n : n * 1000;
    const d = new Date(ms);
    return Number.isNaN(d.getTime()) ? null : d;
  }

  const d = new Date(s);
  return Number.isNaN(d.getTime()) ? null : d;
}

export function formatLocalDateTime(value: DateLike, fallback = '-'): string {
  const d = parseDateLike(value);
  if (!d) {
    const raw = value == null ? '' : String(value).trim();
    return raw || fallback;
  }
  return d.toLocaleString();
}

export function formatLocalTime(value: DateLike, fallback = '--:--:--'): string {
  const d = parseDateLike(value);
  if (!d) {
    const raw = value == null ? '' : String(value).trim();
    return raw || fallback;
  }
  return d.toLocaleTimeString();
}

export function localDateInputValue(base = new Date()): string {
  const d = new Date(base.getTime());
  d.setMinutes(d.getMinutes() - d.getTimezoneOffset());
  return d.toISOString().slice(0, 10);
}

