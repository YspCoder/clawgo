import React, { useEffect, useState, useRef } from 'react';
import { Terminal, Trash2, Play, Square } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import { useUI } from '../context/UIContext';
import EmptyState from '../components/data-display/EmptyState';
import { LogEntry } from '../types';
import { formatLocalTime } from '../utils/time';
import { Button, FixedButton } from '../components/ui/Button';
import PageHeader from '../components/layout/PageHeader';
import ToolbarRow from '../components/layout/ToolbarRow';
import { useLogStream } from '../hooks/useLogStream';

const Logs: React.FC = () => {
  const { t } = useTranslation();
  const ui = useUI();
  const { q } = useAppContext();
  const { logs, isStreaming, setIsStreaming, clearLogs: hookClearLogs } = useLogStream({ q });
  const [codeMap, setCodeMap] = useState<Record<number, string>>({});
  const [showRaw, setShowRaw] = useState(false);
  const logEndRef = useRef<HTMLDivElement>(null);

  const loadCodeMap = async () => {
    try {
      const paths = [`/webui/log-codes.json${q}`, '/log-codes.json'];
      for (const p of paths) {
        const r = await fetch(p);
        if (!r.ok) continue;
        const j = await r.json();
        if (Array.isArray(j?.items)) {
          const m: Record<number, string> = {};
          j.items.forEach((it: any) => {
            if (typeof it?.code === 'number' && typeof it?.text === 'string') {
              m[it.code] = it.text;
            }
          });
          setCodeMap(m);
          return;
        }
      }
    } catch {
      setCodeMap({});
    }
  };

  useEffect(() => {
    loadCodeMap();
  }, [q]);

  useEffect(() => {
    logEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [logs]);

  const clearLogs = async () => {
    const ok = await ui.confirmDialog({
      title: t('logsClearConfirmTitle'),
      message: t('logsClearConfirmMessage'),
      danger: true,
      confirmText: t('clear'),
    });
    if (!ok) return;
    hookClearLogs();
  };

  const toCode = (v: any): number | undefined => {
    if (typeof v === 'number' && Number.isFinite(v) && v > 0) return v;
    if (typeof v === 'string') {
      if (/^L\d{4}$/.test(v)) return Number(v.slice(1));
      const n = Number(v);
      if (Number.isFinite(n) && n > 0) return n;
    }
    return undefined;
  };
  const decode = (v: any) => {
    const c = toCode(v);
    if (!c) return v;
    return codeMap[c] || v;
  };

  const renderReadable = (log: LogEntry) => {
    const keys = Object.keys(log).filter(k => !['time', 'level', 'msg', '__raw'].includes(k));
    const core = `${log.msg}`;
    if (keys.length === 0) return core;
    const extra = keys.map(k => `${k}=${JSON.stringify((log as any)[k])}`).join(' · ');
    return `${core}  |  ${extra}`;
  };

  const getLevelColor = (level: string) => {
    switch ((level || 'INFO').toUpperCase()) {
      case 'ERROR': return 'ui-text-danger';
      case 'WARN': return 'ui-code-warning';
      case 'DEBUG': return 'ui-icon-info';
      default: return 'ui-icon-success';
    }
  };

  return (
    <div className="p-4 md:p-6 xl:p-8 w-full space-y-4 h-full flex flex-col">
      <PageHeader
        title={t('logs')}
        titleClassName="ui-text-primary"
        subtitle={
          <div className={`ui-pill flex items-center gap-1.5 px-2.5 py-0.5 rounded-md text-[10px] font-bold uppercase tracking-wider border ${
            isStreaming ? 'ui-pill-success' : 'ui-pill-neutral'
          }`}>
            <div className={`w-1.5 h-1.5 rounded-full ${isStreaming ? 'ui-dot-live animate-pulse' : 'ui-dot-neutral'}`} />
            {isStreaming ? t('live') : t('paused')}
          </div>
        }
        actions={
          <ToolbarRow>
          <Button onClick={() => setShowRaw(!showRaw)} gap="2">
            {showRaw ? t('pretty') : t('raw')}
          </Button>
          <Button onClick={() => setIsStreaming(!isStreaming)} variant={isStreaming ? 'neutral' : 'primary'} gap="2">
            {isStreaming ? <><Square className="w-4 h-4" /> {t('pause')}</> : <><Play className="w-4 h-4" /> {t('resume')}</>}
          </Button>
          <FixedButton onClick={clearLogs} label={t('clear')}>
            <Trash2 className="w-4 h-4" />
          </FixedButton>
          </ToolbarRow>
        }
      />

      <div className="flex-1 brand-card ui-border-subtle border rounded-[30px] overflow-hidden flex flex-col shadow-2xl">
        <div className="ui-soft-panel ui-border-subtle px-4 py-2 border-b flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Terminal className="ui-icon-muted w-4 h-4" />
            <span className="ui-text-primary text-xs font-mono">{t('systemLog')}</span>
          </div>
          <span className="ui-text-secondary text-[10px] font-mono uppercase tracking-widest">{logs.length} {t('entries')}</span>
        </div>
        <div className="flex-1 overflow-auto selection:bg-indigo-500/30">
          {logs.length === 0 ? (
            <EmptyState
              centered
              className="ui-text-primary h-full p-4"
              icon={<Terminal className="w-8 h-8 opacity-10" />}
              message={t('waitingForLogs')}
            />
          ) : showRaw ? (
            <div className="p-3 font-mono text-xs space-y-1">
              {logs.map((log, i) => (
                <div key={i} className="ui-border-subtle ui-text-primary border-b py-1 break-all">{log.__raw || JSON.stringify(log)}</div>
              ))}
              <div ref={logEndRef} />
            </div>
          ) : (
            <table className="w-full text-xs">
              <thead className="ui-soft-panel ui-border-subtle sticky top-0 border-b">
                <tr className="ui-text-primary">
                  <th className="text-left px-2 py-2 font-semibold">{t('time')}</th>
                  <th className="text-left px-2 py-2 font-semibold">{t('level')}</th>
                  <th className="text-left px-2 py-2 font-semibold">{t('message')}</th>
                  <th className="text-left px-2 py-2 font-semibold">{t('error')}</th>
                  <th className="text-left px-2 py-2 font-semibold">{t('codeCaller')}</th>
                </tr>
              </thead>
              <tbody>
                {logs.map((log, i) => {
                  const lvl = (log.level || 'INFO').toUpperCase();
                  const rawCode = (log as any).code ?? (log as any).message ?? log.msg ?? '';
                  const message = String(decode(rawCode) || '');
                  const errRaw = (log as any).message || (log as any).error || (lvl === 'ERROR' ? rawCode : '');
                  const errText = String(decode(errRaw) || '');
                  const caller = (log as any).caller || (log as any).source || '';
                  const code = toCode(rawCode);
                  return (
                    <tr key={i} className="ui-border-subtle ui-row-hover border-b align-top">
                      <td className="ui-text-secondary px-2 py-1.5 whitespace-nowrap">{formatLocalTime(log.time)}</td>
                      <td className={`px-2 py-1.5 font-semibold whitespace-nowrap ${getLevelColor(lvl)}`}>{lvl}</td>
                      <td className="ui-text-primary px-2 py-1.5 break-all">{message}</td>
                      <td className="ui-text-danger px-2 py-1.5 break-all">{errText}</td>
                      <td className="ui-text-secondary px-2 py-1.5 break-all">{code ? `${code} | ${caller}` : caller}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          )}
          <div ref={logEndRef} />
        </div>
      </div>
    </div>
  );
};

export default Logs;
