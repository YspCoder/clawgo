import React, { createContext, useContext, useState, useEffect, useCallback } from 'react';
import { CronJob, Cfg, Session, Skill } from '../types';

interface AppContextType {
  token: string;
  sidebarOpen: boolean;
  setSidebarOpen: (open: boolean) => void;
  sidebarCollapsed: boolean;
  setSidebarCollapsed: React.Dispatch<React.SetStateAction<boolean>>;
  setToken: (token: string) => void;
  isGatewayOnline: boolean;
  setIsGatewayOnline: (online: boolean) => void;
  cfg: Cfg;
  setCfg: React.Dispatch<React.SetStateAction<Cfg>>;
  cfgRaw: string;
  setCfgRaw: (raw: string) => void;
  configEditing: boolean;
  setConfigEditing: React.Dispatch<React.SetStateAction<boolean>>;
  nodes: string;
  setNodes: (nodes: string) => void;
  nodeTrees: string;
  setNodeTrees: (trees: string) => void;
  cron: CronJob[];
  setCron: (cron: CronJob[]) => void;
  skills: Skill[];
  setSkills: (skills: Skill[]) => void;
  clawhubInstalled: boolean;
  clawhubPath: string;
  sessions: Session[];
  setSessions: React.Dispatch<React.SetStateAction<Session[]>>;
  taskQueueItems: any[];
  setTaskQueueItems: React.Dispatch<React.SetStateAction<any[]>>;
  ekgSummary: Record<string, any>;
  setEkgSummary: React.Dispatch<React.SetStateAction<Record<string, any>>>;
  refreshAll: () => Promise<void>;
  refreshCron: () => Promise<void>;
  refreshNodes: () => Promise<void>;
  refreshSkills: () => Promise<void>;
  refreshSessions: () => Promise<void>;
  refreshTaskQueue: () => Promise<void>;
  refreshEKGSummary: () => Promise<void>;
  refreshVersion: () => Promise<void>;
  loadConfig: (force?: boolean) => Promise<void>;
  gatewayVersion: string;
  webuiVersion: string;
  hotReloadFields: string[];
  hotReloadFieldDetails: Array<{ path: string; name?: string; description?: string }>;
  q: string;
}

const AppContext = createContext<AppContextType | undefined>(undefined);

export const AppProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const initialToken = (() => {
    try {
      const u = new URL(window.location.href);
      return u.searchParams.get('token') || 'cg_nLnov7DPd9yqZDYPEU5pHnoa';
    } catch {
      return 'cg_nLnov7DPd9yqZDYPEU5pHnoa';
    }
  })();
  const [token, setToken] = useState(initialToken);
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [sidebarCollapsed, setSidebarCollapsed] = useState<boolean>(() => {
    try {
      return window.localStorage.getItem('ui.sidebarCollapsed') === '1';
    } catch {
      return false;
    }
  });
  const [isGatewayOnline, setIsGatewayOnline] = useState(true);
  const [cfg, setCfg] = useState<Cfg>({});
  const [cfgRaw, setCfgRaw] = useState('{}');
  const [configEditing, setConfigEditing] = useState(false);
  const [nodes, setNodes] = useState('[]');
  const [nodeTrees, setNodeTrees] = useState('[]');
  const [cron, setCron] = useState<CronJob[]>([]);
  const [skills, setSkills] = useState<Skill[]>([]);
  const [clawhubInstalled, setClawhubInstalled] = useState(false);
  const [clawhubPath, setClawhubPath] = useState('');
  const [sessions, setSessions] = useState<Session[]>([{ key: 'main', title: 'main' }]);
  const [taskQueueItems, setTaskQueueItems] = useState<any[]>([]);
  const [ekgSummary, setEkgSummary] = useState<Record<string, any>>({});
  const [gatewayVersion, setGatewayVersion] = useState('unknown');
  const [webuiVersion, setWebuiVersion] = useState('unknown');
  const [hotReloadFields, setHotReloadFields] = useState<string[]>([]);
  const [hotReloadFieldDetails, setHotReloadFieldDetails] = useState<Array<{ path: string; name?: string; description?: string }>>([]);

  const q = token ? `?token=${encodeURIComponent(token)}` : '';

  const loadConfig = useCallback(async (force = false) => {
    try {
      const hotQ = q ? `${q}&include_hot_reload_fields=1` : '?include_hot_reload_fields=1';
      const r = await fetch(`/webui/api/config${hotQ}`);
      if (!r.ok) throw new Error('Failed to load config');
      const txt = await r.text();
      try {
        const parsed = JSON.parse(txt);
        if (parsed && parsed.config) {
          if (!configEditing || force) {
            setCfg(parsed.config);
            setCfgRaw(JSON.stringify(parsed.config, null, 2));
          }
          setHotReloadFields(Array.isArray(parsed.hot_reload_fields) ? parsed.hot_reload_fields : []);
          setHotReloadFieldDetails(Array.isArray(parsed.hot_reload_field_details) ? parsed.hot_reload_field_details : []);
        } else {
          if (!configEditing || force) {
            setCfg(parsed || {});
            setCfgRaw(txt);
          }
        }
      } catch {
        if (!configEditing || force) {
          setCfgRaw(txt);
          try { setCfg(JSON.parse(txt)); } catch { setCfg({}); }
        }
      }
      setIsGatewayOnline(true);
    } catch (e) {
      setIsGatewayOnline(false);
      console.error(e);
    }
  }, [q, configEditing]);

  const refreshNodes = useCallback(async () => {
    try {
      const r = await fetch(`/webui/api/nodes${q}`);
      if (!r.ok) throw new Error('Failed to load nodes');
      const j = await r.json();
      setNodes(JSON.stringify(j.nodes || [], null, 2));
      setNodeTrees(JSON.stringify(j.trees || [], null, 2));
      setIsGatewayOnline(true);
    } catch (e) {
      setIsGatewayOnline(false);
      console.error(e);
    }
  }, [q]);

  const refreshCron = useCallback(async () => {
    try {
      const r = await fetch(`/webui/api/cron${q}`);
      if (!r.ok) throw new Error('Failed to load cron');
      const j = await r.json();
      setCron(Array.isArray(j.jobs) ? j.jobs : []);
      setIsGatewayOnline(true);
    } catch (e) {
      setIsGatewayOnline(false);
      console.error(e);
    }
  }, [q]);

  const refreshSkills = useCallback(async () => {
    try {
      const r = await fetch(`/webui/api/skills${q}`);
      if (!r.ok) throw new Error('Failed to load skills');
      const j = await r.json();
      setSkills(Array.isArray(j.skills) ? j.skills : []);
      setClawhubInstalled(!!j.clawhub_installed);
      setClawhubPath(typeof j.clawhub_path === 'string' ? j.clawhub_path : '');
      setIsGatewayOnline(true);
    } catch (e) {
      setIsGatewayOnline(false);
      console.error(e);
    }
  }, [q]);

  const refreshSessions = useCallback(async () => {
    try {
      const r = await fetch(`/webui/api/sessions${q}`);
      if (!r.ok) throw new Error('Failed to load sessions');
      const j = await r.json();
      const arr = Array.isArray(j.sessions) ? j.sessions : [];
      setSessions(arr.map((s: any) => ({ key: s.key, title: s.title || s.key })));
      setIsGatewayOnline(true);
    } catch (e) {
      setIsGatewayOnline(false);
      console.error(e);
    }
  }, [q]);

  const refreshVersion = useCallback(async () => {
    try {
      const r = await fetch(`/webui/api/version${q}`);
      if (!r.ok) throw new Error('Failed to load version');
      const j = await r.json();
      setGatewayVersion(j.gateway_version || 'unknown');
      setWebuiVersion(j.webui_version || 'unknown');
    } catch (e) {
      console.error(e);
    }
  }, [q]);

  const refreshTaskQueue = useCallback(async () => {
    try {
      const r = await fetch(`/webui/api/task_queue${q ? `${q}&limit=30` : '?limit=30'}`);
      if (!r.ok) throw new Error('Failed to load task queue');
      const j = await r.json();
      setTaskQueueItems(Array.isArray(j.items) ? j.items : []);
    } catch (e) {
      console.error(e);
    }
  }, [q]);

  const refreshEKGSummary = useCallback(async () => {
    try {
      const r = await fetch(`/webui/api/ekg_stats${q ? `${q}&window=24h` : '?window=24h'}`);
      if (!r.ok) throw new Error('Failed to load ekg summary');
      const j = await r.json();
      setEkgSummary(j && typeof j === 'object' ? j : {});
    } catch (e) {
      console.error(e);
    }
  }, [q]);

  const refreshAll = useCallback(async () => {
    await Promise.all([loadConfig(), refreshCron(), refreshNodes(), refreshSkills(), refreshSessions(), refreshVersion(), refreshTaskQueue(), refreshEKGSummary()]);
  }, [loadConfig, refreshCron, refreshNodes, refreshSkills, refreshSessions, refreshVersion, refreshTaskQueue, refreshEKGSummary]);

  useEffect(() => {
    refreshAll();
    const interval = setInterval(() => {
      loadConfig();
      refreshCron();
      refreshNodes();
      refreshSkills();
      refreshSessions();
      refreshVersion();
      refreshTaskQueue();
      refreshEKGSummary();
    }, 10000);
    return () => clearInterval(interval);
  }, [token, refreshAll, loadConfig, refreshCron, refreshNodes, refreshSkills, refreshSessions, refreshVersion, refreshTaskQueue, refreshEKGSummary]);

  useEffect(() => {
    try {
      window.localStorage.setItem('ui.sidebarCollapsed', sidebarCollapsed ? '1' : '0');
    } catch {
      // ignore persistence failures
    }
  }, [sidebarCollapsed]);

  return (
    <AppContext.Provider value={{
      token, setToken, sidebarOpen, setSidebarOpen, sidebarCollapsed, setSidebarCollapsed, isGatewayOnline, setIsGatewayOnline,
      cfg, setCfg, cfgRaw, setCfgRaw, configEditing, setConfigEditing, nodes, setNodes, nodeTrees, setNodeTrees,
      cron, setCron, skills, setSkills, clawhubInstalled, clawhubPath,
      sessions, setSessions,
      taskQueueItems, setTaskQueueItems, ekgSummary, setEkgSummary,
      refreshAll, refreshCron, refreshNodes, refreshSkills, refreshSessions, refreshTaskQueue, refreshEKGSummary, refreshVersion, loadConfig,
      gatewayVersion, webuiVersion, hotReloadFields, hotReloadFieldDetails, q
    }}>
      {children}
    </AppContext.Provider>
  );
};

export const useAppContext = () => {
  const context = useContext(AppContext);
  if (!context) throw new Error('useAppContext must be used within an AppProvider');
  return context;
};
