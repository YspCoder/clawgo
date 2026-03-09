import React, { Suspense, lazy } from 'react';
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { AppProvider } from './context/AppContext';
import { UIProvider } from './context/UIContext';
import Layout from './components/Layout';

const Dashboard = lazy(() => import('./pages/Dashboard'));
const Chat = lazy(() => import('./pages/Chat'));
const Config = lazy(() => import('./pages/Config'));
const Cron = lazy(() => import('./pages/Cron'));
const Logs = lazy(() => import('./pages/Logs'));
const Skills = lazy(() => import('./pages/Skills'));
const MCP = lazy(() => import('./pages/MCP'));
const ChannelSettings = lazy(() => import('./pages/ChannelSettings'));
const Memory = lazy(() => import('./pages/Memory'));
const Nodes = lazy(() => import('./pages/Nodes'));
const NodeArtifacts = lazy(() => import('./pages/NodeArtifacts'));
const TaskAudit = lazy(() => import('./pages/TaskAudit'));
const EKG = lazy(() => import('./pages/EKG'));
const LogCodes = lazy(() => import('./pages/LogCodes'));
const SubagentProfiles = lazy(() => import('./pages/SubagentProfiles'));
const Subagents = lazy(() => import('./pages/Subagents'));

const pageFallback = (
  <div className="absolute inset-0 flex items-center justify-center bg-zinc-950/80 text-xs tracking-[0.3em] text-zinc-500 uppercase">
    Loading
  </div>
);

export default function App() {
  return (
    <AppProvider>
      <UIProvider>
        <BrowserRouter basename="/webui">
          <Suspense fallback={pageFallback}>
            <Routes>
              <Route path="/" element={<Layout />}>
                <Route index element={<Dashboard />} />
                <Route path="chat" element={<Chat />} />
                <Route path="logs" element={<Logs />} />
                <Route path="log-codes" element={<LogCodes />} />
                <Route path="skills" element={<Skills />} />
                <Route path="mcp" element={<MCP />} />
                <Route path="whatsapp" element={<Navigate to="/channels/whatsapp" replace />} />
                <Route path="channels/:channelId" element={<ChannelSettings />} />
                <Route path="config" element={<Config />} />
                <Route path="cron" element={<Cron />} />
                <Route path="memory" element={<Memory />} />
                <Route path="nodes" element={<Nodes />} />
                <Route path="node-artifacts" element={<NodeArtifacts />} />
                <Route path="task-audit" element={<TaskAudit />} />
                <Route path="ekg" element={<EKG />} />
                <Route path="subagent-profiles" element={<SubagentProfiles />} />
                <Route path="subagents" element={<Subagents />} />
              </Route>
            </Routes>
          </Suspense>
        </BrowserRouter>
      </UIProvider>
    </AppProvider>
  );
}
