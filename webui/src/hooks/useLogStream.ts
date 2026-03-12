import { useState, useRef, useEffect, useCallback } from 'react';
import { LogEntry } from '../types';

interface UseLogStreamOptions {
  q: string;
}

export function useLogStream({ q }: UseLogStreamOptions) {
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [isStreaming, setIsStreaming] = useState(true);
  const socketRef = useRef<WebSocket | null>(null);
  const reconnectTimeoutRef = useRef<number | null>(null);

  const normalizeLog = useCallback((v: any): LogEntry => ({
    time: typeof v?.time === 'string' && v.time ? v.time : (typeof v?.timestamp === 'string' && v.timestamp ? v.timestamp : new Date().toISOString()),
    level: typeof v?.level === 'string' && v.level ? v.level : 'INFO',
    code: typeof v?.code === 'number' ? v.code : undefined,
    msg: typeof v?.msg === 'string' ? v.msg : (typeof v?.message === 'string' ? v.message : JSON.stringify(v)),
    __raw: JSON.stringify(v),
    ...v,
  }), []);

  const loadRecent = useCallback(async () => {
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
  }, [q, normalizeLog]);

  const closeSocket = useCallback(() => {
    if (reconnectTimeoutRef.current) {
      clearTimeout(reconnectTimeoutRef.current);
      reconnectTimeoutRef.current = null;
    }
    if (socketRef.current) {
      socketRef.current.close();
      socketRef.current = null;
    }
  }, []);

  const startStreaming = useCallback(() => {
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
    ws.onclose = (event) => {
      if (socketRef.current === ws) {
        socketRef.current = null;
        if (event.code !== 1000 && event.code !== 1005) { // 1000 is Normal Closure, 1005 is No Status Received
          reconnectTimeoutRef.current = window.setTimeout(() => {
            startStreaming();
          }, 3000);
        }
      }
    };
  }, [q, normalizeLog, closeSocket]);

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
  }, [isStreaming, q, loadRecent, startStreaming, closeSocket]);

  const clearLogs = useCallback(() => {
    setLogs([]);
  }, []);

  return {
    logs,
    isStreaming,
    setIsStreaming,
    clearLogs
  };
}
