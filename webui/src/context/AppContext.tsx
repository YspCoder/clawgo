import React, { createContext, useContext, useState, useEffect, useCallback } from 'react';
import { CronJob, Cfg, Session, Skill } from '../types';

interface AppContextType {
  token: string;
  setToken: (token: string) => void;
  isGatewayOnline: boolean;
  setIsGatewayOnline: (online: boolean) => void;
  cfg: Cfg;
  setCfg: React.Dispatch<React.SetStateAction<Cfg>>;
  cfgRaw: string;
  setCfgRaw: (raw: string) => void;
  nodes: string;
  setNodes: (nodes: string) => void;
  cron: CronJob[];
  setCron: (cron: CronJob[]) => void;
  skills: Skill[];
  setSkills: (skills: Skill[]) => void;
  sessions: Session[];
  setSessions: React.Dispatch<React.SetStateAction<Session[]>>;
  refreshAll: () => Promise<void>;
  refreshCron: () => Promise<void>;
  refreshNodes: () => Promise<void>;
  refreshSkills: () => Promise<void>;
  loadConfig: () => Promise<void>;
  q: string;
}

const AppContext = createContext<AppContextType | undefined>(undefined);

export const AppProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const [token, setToken] = useState('cg_nLnov7DPd9yqZDYPEU5pHnoa');
  const [isGatewayOnline, setIsGatewayOnline] = useState(true);
  const [cfg, setCfg] = useState<Cfg>({});
  const [cfgRaw, setCfgRaw] = useState('{}');
  const [nodes, setNodes] = useState('[]');
  const [cron, setCron] = useState<CronJob[]>([]);
  const [skills, setSkills] = useState<Skill[]>([]);
  const [sessions, setSessions] = useState<Session[]>([{ key: 'webui:default', title: 'Default' }]);

  const q = token ? `?token=${encodeURIComponent(token)}` : '';

  const loadConfig = useCallback(async () => {
    try {
      const r = await fetch(`/webui/api/config${q}`);
      if (!r.ok) throw new Error('Failed to load config');
      const txt = await r.text();
      setCfgRaw(txt);
      try { setCfg(JSON.parse(txt)); } catch { setCfg({}); }
      setIsGatewayOnline(true);
    } catch (e) {
      setIsGatewayOnline(false);
      console.error(e);
    }
  }, [q]);

  const refreshNodes = useCallback(async () => {
    try {
      const r = await fetch(`/webui/api/nodes${q}`);
      if (!r.ok) throw new Error('Failed to load nodes');
      const j = await r.json();
      setNodes(JSON.stringify(j.nodes || [], null, 2));
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
      setIsGatewayOnline(true);
    } catch (e) {
      setIsGatewayOnline(false);
      console.error(e);
    }
  }, [q]);

  const refreshAll = useCallback(async () => {
    await Promise.all([loadConfig(), refreshCron(), refreshNodes(), refreshSkills()]);
  }, [loadConfig, refreshCron, refreshNodes, refreshSkills]);

  useEffect(() => {
    refreshAll();
    const interval = setInterval(() => {
      refreshCron();
      refreshNodes();
      refreshSkills();
    }, 10000);
    return () => clearInterval(interval);
  }, [token, refreshAll, refreshCron, refreshNodes, refreshSkills]);

  return (
    <AppContext.Provider value={{
      token, setToken, isGatewayOnline, setIsGatewayOnline,
      cfg, setCfg, cfgRaw, setCfgRaw, nodes, setNodes,
      cron, setCron, skills, setSkills,
      sessions, setSessions,
      refreshAll, refreshCron, refreshNodes, refreshSkills, loadConfig, q
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
