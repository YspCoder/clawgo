import React, { useEffect, useState, useRef } from 'react';
import { Terminal, Trash2, Play, Square } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import { useUI } from '../context/UIContext';
import { LogEntry } from '../types';
import { formatLocalTime } from '../utils/time';

const Logs: React.FC = () => {
  const { t } = useTranslation();
  const ui = useUI();
  const { q } = useAppContext();
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [codeMap, setCodeMap] = useState<Record<number, string>>({});
  const [isStreaming, setIsStreaming] = useState(true);
  const [showRaw, setShowRaw] = useState(false);
  const logEndRef = useRef<HTMLDivElement>(null);
  const socketRef = useRef<WebSocket | null>(null);

  const loadRecent = async () => {
    try {
      const r = await fetch(`/webui/api/logs/recent${q ? `${q}&limit=10` : '?limit=10'}`);
      if (!r.ok) return;
      const j = await r.json();
      if (Array.isArray(j.logs)) {
        setLogs(j.logs.map(normalizeLog));
      }
    } catch (e) {
      console.error('L0096', e);
    }
  };

  const closeSocket = () => {
    if (socketRef.current) {
      socketRef.current.close();
      socketRef.current = null;
    }
  };

  const startStreaming = () => {
    closeSocket();
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const url = new URL(`${proto}//${window.location.host}/webui/api/logs/live`);
    const token = new URLSearchParams(q.startsWith('?') ? q.slice(1) : q).get('token');
    if (token) url.searchParams.set('token', token);

    const ws = new WebSocket(url.toString());
    socketRef.current = ws;
    ws.onmessage = (event) => {
      try {
        const payload = JSON.parse(event.data);
        const log = normalizeLog(payload?.entry ?? payload);
        setLogs(prev => [...prev.slice(-1000), log]);
      } catch (e) {
        console.error('L0097', e);
      }
    };
    ws.onerror = (e) => {
      console.error('L0097', e);
    };
    ws.onclose = () => {
      if (socketRef.current === ws) {
        socketRef.current = null;
      }
    };
  };

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
    loadRecent();
    if (isStreaming) {
      startStreaming();
    } else {
      closeSocket();
    }

    return () => {
      closeSocket();
    };
  }, [isStreaming, q]);

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
    setLogs([]);
  };

  const normalizeLog = (v: any): LogEntry => ({
    time: typeof v?.time === 'string' && v.time ? v.time : (typeof v?.timestamp === 'string' && v.timestamp ? v.timestamp : new Date().toISOString()),
    level: typeof v?.level === 'string' && v.level ? v.level : 'INFO',
    code: typeof v?.code === 'number' ? v.code : undefined,
    msg: typeof v?.msg === 'string' ? v.msg : (typeof v?.message === 'string' ? v.message : JSON.stringify(v)),
    __raw: JSON.stringify(v),
    ...v,
  });

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
    <div className="p-4 md:p-5 xl:p-6 w-full space-y-4 h-full flex flex-col">
      <div className="flex items-start justify-between gap-3 flex-wrap">
        <div className="flex items-center gap-3">
          <h1 className="ui-text-primary text-2xl font-semibold tracking-tight">{t('logs')}</h1>
          <div className={`ui-pill flex items-center gap-1.5 px-2.5 py-0.5 rounded-md text-[10px] font-bold uppercase tracking-wider border ${
            isStreaming ? 'ui-pill-success' : 'ui-pill-neutral'
          }`}>
            <div className={`w-1.5 h-1.5 rounded-full ${isStreaming ? 'ui-dot-live animate-pulse' : 'ui-dot-neutral'}`} />
            {isStreaming ? t('live') : t('paused')}
          </div>
        </div>
        <div className="flex items-center gap-2 flex-wrap">
          <button
            onClick={() => setShowRaw(!showRaw)}
            className="ui-button ui-button-neutral flex items-center gap-2 px-4 py-2 text-sm font-medium"
          >
            {showRaw ? t('pretty') : t('raw')}
          </button>
          <button 
            onClick={() => setIsStreaming(!isStreaming)}
            className={`ui-button flex items-center gap-2 px-4 py-2 text-sm font-medium ${
              isStreaming ? 'ui-button-neutral' : 'ui-button-primary'
            }`}
          >
            {isStreaming ? <><Square className="w-4 h-4" /> {t('pause')}</> : <><Play className="w-4 h-4" /> {t('resume')}</>}
          </button>
          <button onClick={clearLogs} className="ui-button ui-button-neutral flex items-center gap-2 px-4 py-2 text-sm font-medium">
            <Trash2 className="w-4 h-4" /> {t('clear')}
          </button>
        </div>
      </div>

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
            <div className="ui-text-primary h-full flex flex-col items-center justify-center space-y-2 p-4">
              <Terminal className="w-8 h-8 opacity-10" />
              <p>{t('waitingForLogs')}</p>
            </div>
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
