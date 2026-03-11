import { cloneJSON } from '../../utils/object';

export type RuntimeWindow = 'all' | '1h' | '24h' | '7d';

export function setPath(obj: any, path: string, value: any) {
  const keys = path.split('.');
  const next = cloneJSON(obj || {});
  let cur = next;
  for (let i = 0; i < keys.length - 1; i++) {
    const key = keys[i];
    if (typeof cur[key] !== 'object' || cur[key] === null) cur[key] = {};
    cur = cur[key];
  }
  cur[keys[keys.length - 1]] = value;
  return next;
}

export function parseTagRuleText(raw: string) {
  const out: Record<string, string[]> = {};
  for (const line of String(raw || '').split('\n')) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    const idx = trimmed.indexOf('=');
    if (idx <= 0) continue;
    const key = trimmed.slice(0, idx).trim();
    const tags = trimmed.slice(idx + 1).split(',').map((item) => item.trim()).filter(Boolean);
    if (!key || tags.length === 0) continue;
    out[key] = tags;
  }
  return out;
}

export function formatTagRuleText(value: unknown) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return '';
  return Object.entries(value as Record<string, any>)
    .map(([key, tags]) => `${key}=${Array.isArray(tags) ? tags.join(',') : ''}`)
    .filter((line) => line !== '=')
    .join('\n');
}

export function runtimeCutoff(windowKey: RuntimeWindow) {
  if (windowKey === 'all') return 0;
  const now = Date.now();
  if (windowKey === '1h') return now - 60 * 60 * 1000;
  if (windowKey === '24h') return now - 24 * 60 * 60 * 1000;
  return now - 7 * 24 * 60 * 60 * 1000;
}

export function filterRuntimeEventsByWindow(items: any[], windowKey: RuntimeWindow) {
  if (!Array.isArray(items)) return [];
  const cutoff = runtimeCutoff(windowKey);
  if (!cutoff) return items;
  return items.filter((item) => {
    const when = Date.parse(String(item?.when || ''));
    return Number.isFinite(when) && when >= cutoff;
  });
}

export function buildDiffRows(
  baseline: any,
  currentPayload: any,
  rootLabel: string,
) {
  const out: Array<{ path: string; before: any; after: any }> = [];
  const walk = (a: any, b: any, path: string) => {
    const keys = new Set([
      ...(a && typeof a === 'object' ? Object.keys(a) : []),
      ...(b && typeof b === 'object' ? Object.keys(b) : []),
    ]);
    if (keys.size === 0) {
      if (JSON.stringify(a) !== JSON.stringify(b)) out.push({ path: path || rootLabel, before: a, after: b });
      return;
    }
    keys.forEach((key) => {
      const nextPath = path ? `${path}.${key}` : key;
      const before = a ? a[key] : undefined;
      const after = b ? b[key] : undefined;
      const bothObj = before && after && typeof before === 'object' && typeof after === 'object' && !Array.isArray(before) && !Array.isArray(after);
      if (bothObj) walk(before, after, nextPath);
      else if (JSON.stringify(before) !== JSON.stringify(after)) out.push({ path: nextPath, before, after });
    });
  };
  walk(baseline || {}, currentPayload || {}, '');
  return out;
}

export function createDefaultProxyConfig() {
  return {
    api_key: '',
    api_base: '',
    models: [],
    responses: {
      web_search_enabled: false,
      web_search_context_size: '',
      file_search_vector_store_ids: [],
      file_search_max_num_results: 0,
      include: [],
      stream_include_usage: false,
    },
    supports_responses_compact: false,
    auth: 'oauth',
    timeout_sec: 120,
  };
}

export function buildProviderRuntimeExportPayload(name: string, item: any) {
  return {
    name,
    auth: item?.auth,
    api_state: item?.api_state,
    candidate_order: item?.candidate_order || [],
    last_success: item?.last_success || null,
    recent_hits: item?.recent_hits || [],
    recent_errors: item?.recent_errors || [],
    oauth_accounts: item?.oauth_accounts || [],
  };
}
