export function formatRuntimeTime(value: unknown) {
  const raw = String(value || '').trim();
  if (!raw || raw === '0001-01-01T00:00:00Z') return '-';
  const ts = Date.parse(raw);
  if (Number.isNaN(ts)) return raw;
  return new Date(ts).toLocaleString();
}
