import React, { useEffect, useMemo, useState } from 'react';
import { RefreshCw } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useSearchParams } from 'react-router-dom';
import { useAppContext } from '../context/AppContext';
import { formatLocalDateTime } from '../utils/time';

function dataUrlForArtifact(artifact: any) {
  const mime = String(artifact?.mime_type || '').trim() || 'application/octet-stream';
  const content = String(artifact?.content_base64 || '').trim();
  if (!content) return '';
  return `data:${mime};base64,${content}`;
}

function formatBytes(value: unknown) {
  const size = Number(value || 0);
  if (!Number.isFinite(size) || size <= 0) return '-';
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
  return `${(size / (1024 * 1024)).toFixed(1)} MB`;
}

const NodeArtifacts: React.FC = () => {
  const { t } = useTranslation();
  const { q } = useAppContext();
  const [searchParams, setSearchParams] = useSearchParams();
  const [items, setItems] = useState<any[]>([]);
  const [retentionSummary, setRetentionSummary] = useState<Record<string, any>>({});
  const [selectedID, setSelectedID] = useState('');
  const [loading, setLoading] = useState(false);
  const [prunePending, setPrunePending] = useState(false);
  const [nodeFilter, setNodeFilter] = useState(searchParams.get('node') || 'all');
  const [actionFilter, setActionFilter] = useState(searchParams.get('action') || 'all');
  const [kindFilter, setKindFilter] = useState(searchParams.get('kind') || 'all');
  const [keepLatest, setKeepLatest] = useState('20');

  const apiQuery = useMemo(() => {
    const params = new URLSearchParams();
    params.set('limit', '400');
    if (nodeFilter !== 'all') params.set('node', nodeFilter);
    if (actionFilter !== 'all') params.set('action', actionFilter);
    if (kindFilter !== 'all') params.set('kind', kindFilter);
    return params.toString();
  }, [nodeFilter, actionFilter, kindFilter]);

  useEffect(() => {
    const next = new URLSearchParams();
    if (nodeFilter !== 'all') next.set('node', nodeFilter);
    if (actionFilter !== 'all') next.set('action', actionFilter);
    if (kindFilter !== 'all') next.set('kind', kindFilter);
    setSearchParams(next, { replace: true });
  }, [nodeFilter, actionFilter, kindFilter, setSearchParams]);

  const loadArtifacts = async () => {
    setLoading(true);
    try {
      const query = q ? `${q}&${apiQuery}` : `?${apiQuery}`;
      const r = await fetch(`/webui/api/node_artifacts${query}`);
      if (!r.ok) throw new Error(await r.text());
      const j = await r.json();
      const next = Array.isArray(j.items) ? j.items : [];
      setRetentionSummary(j.artifact_retention && typeof j.artifact_retention === 'object' ? j.artifact_retention : {});
      setItems(next);
      if (next.length === 0) {
        setSelectedID('');
      } else if (!next.some((item: any) => String(item?.id || '') === selectedID)) {
        setSelectedID(String(next[0]?.id || ''));
      }
    } catch (err) {
      console.error(err);
      setItems([]);
      setSelectedID('');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadArtifacts();
  }, [q, apiQuery]);

  const nodes = useMemo(() => Array.from(new Set(items.map((item) => String(item?.node || '')).filter(Boolean))).sort(), [items]);
  const actions = useMemo(() => Array.from(new Set(items.map((item) => String(item?.action || '')).filter(Boolean))).sort(), [items]);
  const kinds = useMemo(() => Array.from(new Set(items.map((item) => String(item?.kind || '')).filter(Boolean))).sort(), [items]);

  const filteredItems = items;

  const selected = useMemo(() => {
    return filteredItems.find((item) => String(item?.id || '') === selectedID) || filteredItems[0] || null;
  }, [filteredItems, selectedID]);

  async function deleteArtifact(id: string) {
    const r = await fetch(`/webui/api/node_artifacts/delete${q}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id }),
    });
    if (!r.ok) throw new Error(await r.text());
    await loadArtifacts();
  }

  async function pruneArtifacts() {
    setPrunePending(true);
    try {
      const keep = Math.max(0, Number.parseInt(keepLatest || '0', 10) || 0);
      const r = await fetch(`/webui/api/node_artifacts/prune${q}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          node: nodeFilter === 'all' ? '' : nodeFilter,
          action: actionFilter === 'all' ? '' : actionFilter,
          kind: kindFilter === 'all' ? '' : kindFilter,
          keep_latest: keep,
          limit: 1000,
        }),
      });
      if (!r.ok) throw new Error(await r.text());
      const j = await r.json();
      setRetentionSummary(j && typeof j === 'object' ? { ...retentionSummary, manual_pruned: j.pruned, manual_deleted_files: j.deleted_files, last_run_at: new Date().toISOString() } : retentionSummary);
      await loadArtifacts();
    } catch (err) {
      console.error(err);
    } finally {
      setPrunePending(false);
    }
  }

  function downloadURL(id: string) {
    return `/webui/api/node_artifacts/download${q ? `${q}&id=${encodeURIComponent(id)}` : `?id=${encodeURIComponent(id)}`}`;
  }

  function exportURL() {
    const query = q ? `${q}&${apiQuery}` : `?${apiQuery}`;
    return `/webui/api/node_artifacts/export${query}`;
  }

  return (
    <div className="h-full p-4 md:p-6 xl:p-8 flex flex-col gap-4">
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div>
          <h1 className="ui-text-primary text-xl md:text-2xl font-semibold">{t('nodeArtifacts')}</h1>
          <div className="ui-text-muted text-sm mt-1">{t('nodeArtifactsHint')}</div>
        </div>
        <div className="flex items-center gap-2">
          <a href={exportURL()} className="ui-button ui-button-neutral px-3 py-1.5 text-sm">
            {t('export')}
          </a>
          <button
            onClick={loadArtifacts}
            className="ui-button ui-button-primary ui-button-icon"
            title={loading ? t('loading') : t('refresh')}
            aria-label={loading ? t('loading') : t('refresh')}
          >
            <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
          </button>
        </div>
      </div>

      <div className="grid grid-cols-1 xl:grid-cols-[320px_1fr] gap-4 flex-1 min-h-0">
        <div className="brand-card ui-panel rounded-[28px] overflow-hidden flex flex-col min-h-0">
          <div className="ui-border-subtle p-3 border-b space-y-2">
            <div className="ui-code-panel ui-text-secondary p-3 text-xs space-y-1">
              <div className="ui-text-primary font-medium">{t('nodeArtifactsRetention')}</div>
              <div>{t('nodeArtifactsRetentionKeepLatest')}: {Number(retentionSummary?.keep_latest || 0) || '-'}</div>
              <div>{t('nodeArtifactsRetentionRetainDays')}: {Number(retentionSummary?.retain_days || 0)}</div>
              <div>{t('nodeArtifactsRetentionPruned')}: {Number(retentionSummary?.pruned || retentionSummary?.manual_pruned || 0)}</div>
              <div>{t('nodeArtifactsRetentionRemaining')}: {Number(retentionSummary?.remaining || filteredItems.length || 0)}</div>
            </div>
            <div className="grid grid-cols-1 gap-2">
              <select value={nodeFilter} onChange={(e) => setNodeFilter(e.target.value)} className="ui-select rounded-xl px-2 py-2 text-xs">
                <option value="all">{t('allNodes')}</option>
                {nodes.map((node) => <option key={node} value={node}>{node}</option>)}
              </select>
              <select value={actionFilter} onChange={(e) => setActionFilter(e.target.value)} className="ui-select rounded-xl px-2 py-2 text-xs">
                <option value="all">{t('allActions')}</option>
                {actions.map((action) => <option key={action} value={action}>{action}</option>)}
              </select>
              <select value={kindFilter} onChange={(e) => setKindFilter(e.target.value)} className="ui-select rounded-xl px-2 py-2 text-xs">
                <option value="all">{t('allKinds')}</option>
                {kinds.map((kind) => <option key={kind} value={kind}>{kind}</option>)}
              </select>
            </div>
            <div className="grid grid-cols-[1fr_auto] gap-2">
              <input
                value={keepLatest}
                onChange={(e) => setKeepLatest(e.target.value)}
                inputMode="numeric"
                placeholder={t('nodeArtifactsKeepLatest')}
                className="ui-input rounded-xl px-3 py-2 text-xs"
              />
              <button
                onClick={pruneArtifacts}
                disabled={prunePending}
                className="ui-button ui-button-warning px-3 py-2 text-xs"
              >
                {prunePending ? t('loading') : t('nodeArtifactsPrune')}
              </button>
            </div>
          </div>
          <div className="overflow-y-auto min-h-0">
            {filteredItems.length === 0 ? (
              <div className="ui-text-muted p-4 text-sm">{t('nodeArtifactsEmpty')}</div>
            ) : filteredItems.map((item, index) => {
              const active = String(selected?.id || '') === String(item?.id || '');
              return (
                <button
                  key={String(item?.id || index)}
                  onClick={() => setSelectedID(String(item?.id || ''))}
                  className={`ui-border-subtle w-full text-left px-3 py-3 border-b ${active ? 'ui-card-active-warning' : 'ui-row-hover'}`}
                >
                  <div className="ui-text-primary text-sm font-medium truncate">{String(item?.name || item?.source_path || `artifact-${index + 1}`)}</div>
                  <div className="ui-text-subtle text-xs truncate">{String(item?.node || '-')} · {String(item?.action || '-')} · {String(item?.kind || '-')}</div>
                  <div className="ui-text-muted text-[11px] truncate">{formatLocalDateTime(item?.time)}</div>
                </button>
              );
            })}
          </div>
        </div>

        <div className="brand-card ui-panel rounded-[28px] overflow-hidden flex flex-col min-h-0">
          <div className="ui-border-subtle ui-text-subtle px-3 py-2 border-b text-xs uppercase tracking-wider">{t('nodeArtifactDetail')}</div>
          <div className="p-4 overflow-y-auto min-h-0 space-y-4 text-sm">
            {!selected ? (
              <div className="ui-text-muted">{t('nodeArtifactsEmpty')}</div>
            ) : (
              <>
                <div className="flex items-center justify-between gap-3">
                  <div>
                    <div className="ui-text-primary text-lg font-medium">{String(selected?.name || selected?.source_path || 'artifact')}</div>
                    <div className="ui-text-muted text-xs mt-1">{String(selected?.node || '-')} · {String(selected?.action || '-')} · {formatLocalDateTime(selected?.time)}</div>
                  </div>
                  <div className="flex items-center gap-2">
                    <a href={downloadURL(String(selected?.id || ''))} className="ui-button ui-button-neutral px-3 py-1.5 text-xs">
                      {t('download')}
                    </a>
                    <button onClick={() => deleteArtifact(String(selected?.id || ''))} className="ui-button ui-button-danger px-3 py-1.5 text-xs">
                      {t('delete')}
                    </button>
                  </div>
                </div>

                <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
                  <div><div className="ui-text-muted text-xs">{t('node')}</div><div className="ui-text-secondary">{String(selected?.node || '-')}</div></div>
                  <div><div className="ui-text-muted text-xs">{t('action')}</div><div className="ui-text-secondary">{String(selected?.action || '-')}</div></div>
                  <div><div className="ui-text-muted text-xs">{t('kind')}</div><div className="ui-text-secondary">{String(selected?.kind || '-')}</div></div>
                  <div><div className="ui-text-muted text-xs">{t('size')}</div><div className="ui-text-secondary">{formatBytes(selected?.size_bytes)}</div></div>
                </div>

                <div className="ui-text-muted text-xs break-all">
                  {String(selected?.source_path || selected?.path || selected?.url || '-')}
                </div>

                {(() => {
                  const kind = String(selected?.kind || '').trim().toLowerCase();
                  const mime = String(selected?.mime_type || '').trim().toLowerCase();
                  const isImage = kind === 'image' || mime.startsWith('image/');
                  const isVideo = kind === 'video' || mime.startsWith('video/');
                  const dataUrl = dataUrlForArtifact(selected);
                  if (isImage && dataUrl) {
                    return <img src={dataUrl} alt={String(selected?.name || 'artifact')} className="ui-media-surface-strong max-h-[420px] rounded-2xl border object-contain" />;
                  }
                  if (isVideo && dataUrl) {
                    return <video src={dataUrl} controls className="ui-media-surface-strong max-h-[420px] w-full rounded-2xl border" />;
                  }
                  if (String(selected?.content_text || '').trim() !== '') {
                    return <pre className="ui-code-panel p-3 text-[12px] whitespace-pre-wrap overflow-auto max-h-[420px]">{String(selected?.content_text || '')}</pre>;
                  }
                  return <div className="ui-text-muted">{t('nodeArtifactPreviewUnavailable')}</div>;
                })()}

                <pre className="ui-code-panel p-3 text-xs overflow-auto">{JSON.stringify(selected, null, 2)}</pre>
              </>
            )}
          </div>
        </div>
      </div>
    </div>
  );
};

export default NodeArtifacts;
