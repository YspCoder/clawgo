import React, { useEffect, useState, useRef } from 'react';
import { Terminal, Trash2, Play, Square } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import { LogEntry } from '../types';

const Logs: React.FC = () => {
  const { t } = useTranslation();
  const { q } = useAppContext();
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [codeMap, setCodeMap] = useState<Record<number, string>>({});
  const [isStreaming, setIsStreaming] = useState(true);
  const [showRaw, setShowRaw] = useState(false);
  const logEndRef = useRef<HTMLDivElement>(null);
  const abortControllerRef = useRef<AbortController | null>(null);

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

  const startStreaming = async () => {
    if (abortControllerRef.current) abortControllerRef.current.abort();
    abortControllerRef.current = new AbortController();

    try {
      const response = await fetch(`/webui/api/logs/stream${q}`, {
        signal: abortControllerRef.current.signal,
      });

      if (!response.body) return;

      const reader = response.body.getReader();
      const decoder = new TextDecoder();

      while (true) {
        const { value, done } = await reader.read();
        if (done) break;

        const chunk = decoder.decode(value, { stream: true });
        const lines = chunk.split('\n').filter(line => line.trim());

        lines.forEach(line => {
          try {
            const log = normalizeLog(JSON.parse(line));
            setLogs(prev => [...prev.slice(-1000), log]);
          } catch (e) {
            // Fallback for non-JSON logs
            setLogs(prev => [...prev.slice(-1000), normalizeLog({ time: new Date().toISOString(), level: 'INFO', msg: line })]);
          }
        });
      }
    } catch (e: any) {
      if (e.name !== 'AbortError') {
        console.error('L0097', e);
      }
    }
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
      if (abortControllerRef.current) abortControllerRef.current.abort();
    }

    return () => {
      if (abortControllerRef.current) abortControllerRef.current.abort();
    };
  }, [isStreaming, q]);

  useEffect(() => {
    logEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [logs]);

  const clearLogs = () => setLogs([]);

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

  const formatTime = (raw: string) => {
    try {
      if (!raw) return '--:--:--';
      if (raw.includes('T')) {
        const right = raw.split('T')[1] || '';
        return (right.split('.')[0] || right).trim() || '--:--:--';
      }
      return raw;
    } catch {
      return '--:--:--';
    }
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
      case 'ERROR': return 'text-red-400';
      case 'WARN': return 'text-amber-400';
      case 'DEBUG': return 'text-blue-400';
      default: return 'text-emerald-400';
    }
  };

  return (
    <div className="p-8 max-w-7xl mx-auto space-y-6 h-full flex flex-col">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <h1 className="text-2xl font-semibold tracking-tight">{t('logs')}</h1>
          <div className={`flex items-center gap-1.5 px-2.5 py-0.5 rounded-md text-[10px] font-bold uppercase tracking-wider border ${
            isStreaming ? 'bg-emerald-500/10 text-emerald-400 border-emerald-500/20' : 'bg-zinc-800 text-zinc-500 border-zinc-700'
          }`}>
            <div className={`w-1.5 h-1.5 rounded-full ${isStreaming ? 'bg-emerald-500 animate-pulse' : 'bg-zinc-600'}`} />
            {isStreaming ? t('live') : t('paused')}
          </div>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={() => setShowRaw(!showRaw)}
            className="flex items-center gap-2 px-4 py-2 bg-zinc-800 hover:bg-zinc-700 rounded-lg text-sm font-medium transition-colors text-zinc-300"
          >
            {showRaw ? t('pretty') : t('raw')}
          </button>
          <button 
            onClick={() => setIsStreaming(!isStreaming)}
            className={`flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors ${
              isStreaming ? 'bg-zinc-800 hover:bg-zinc-700 text-zinc-300' : 'bg-indigo-600 hover:bg-indigo-500 text-white'
            }`}
          >
            {isStreaming ? <><Square className="w-4 h-4" /> {t('pause')}</> : <><Play className="w-4 h-4" /> {t('resume')}</>}
          </button>
          <button onClick={clearLogs} className="flex items-center gap-2 px-4 py-2 bg-zinc-800 hover:bg-zinc-700 rounded-lg text-sm font-medium transition-colors text-zinc-300">
            <Trash2 className="w-4 h-4" /> {t('clear')}
          </button>
        </div>
      </div>

      <div className="flex-1 bg-zinc-950 border border-zinc-800 rounded-2xl overflow-hidden flex flex-col shadow-2xl">
        <div className="bg-zinc-900/50 px-4 py-2 border-b border-zinc-800 flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Terminal className="w-4 h-4 text-zinc-500" />
            <span className="text-xs font-mono text-zinc-500">{t('systemLog')}</span>
          </div>
          <span className="text-[10px] font-mono text-zinc-600 uppercase tracking-widest">{logs.length} {t('entries')}</span>
        </div>
        <div className="flex-1 overflow-auto selection:bg-indigo-500/30">
          {logs.length === 0 ? (
            <div className="h-full flex flex-col items-center justify-center text-zinc-700 space-y-2 p-4">
              <Terminal className="w-8 h-8 opacity-10" />
              <p>{t('waitingForLogs')}</p>
            </div>
          ) : showRaw ? (
            <div className="p-3 font-mono text-xs space-y-1">
              {logs.map((log, i) => (
                <div key={i} className="border-b border-zinc-900 py-1 text-zinc-300 break-all">{log.__raw || JSON.stringify(log)}</div>
              ))}
              <div ref={logEndRef} />
            </div>
          ) : (
            <table className="w-full text-xs">
              <thead className="sticky top-0 bg-zinc-900/95 border-b border-zinc-800">
                <tr className="text-zinc-400">
                  <th className="text-left p-2 font-medium">{t('time')}</th>
                  <th className="text-left p-2 font-medium">{t('level')}</th>
                  <th className="text-left p-2 font-medium">{t('message')}</th>
                  <th className="text-left p-2 font-medium">{t('error')}</th>
                  <th className="text-left p-2 font-medium">{t('codeCaller')}</th>
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
                    <tr key={i} className="border-b border-zinc-900 hover:bg-zinc-900/40 align-top">
                      <td className="p-2 text-zinc-500 whitespace-nowrap">{formatTime(log.time)}</td>
                      <td className={`p-2 font-semibold whitespace-nowrap ${getLevelColor(lvl)}`}>{lvl}</td>
                      <td className="p-2 text-zinc-200 break-all">{message}</td>
                      <td className="p-2 text-red-300 break-all">{errText}</td>
                      <td className="p-2 text-zinc-500 break-all">{code ? `${code} | ${caller}` : caller}</td>
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
