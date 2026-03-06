import React from 'react';
import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { AppProvider } from './context/AppContext';
import { UIProvider } from './context/UIContext';
import Layout from './components/Layout';
import Dashboard from './pages/Dashboard';
import Chat from './pages/Chat';
import Config from './pages/Config';
import Cron from './pages/Cron';
import Logs from './pages/Logs';
import Skills from './pages/Skills';
import Memory from './pages/Memory';
import TaskAudit from './pages/TaskAudit';
import EKG from './pages/EKG';
import LogCodes from './pages/LogCodes';
import SubagentProfiles from './pages/SubagentProfiles';
import Subagents from './pages/Subagents';

export default function App() {
  return (
    <AppProvider>
      <UIProvider>
        <BrowserRouter basename="/webui">
          <Routes>
            <Route path="/" element={<Layout />}>
              <Route index element={<Dashboard />} />
              <Route path="chat" element={<Chat />} />
              <Route path="logs" element={<Logs />} />
              <Route path="log-codes" element={<LogCodes />} />
              <Route path="skills" element={<Skills />} />
              <Route path="config" element={<Config />} />
              <Route path="cron" element={<Cron />} />
              <Route path="memory" element={<Memory />} />
              <Route path="task-audit" element={<TaskAudit />} />
              <Route path="ekg" element={<EKG />} />
              <Route path="subagent-profiles" element={<SubagentProfiles />} />
              <Route path="subagents" element={<Subagents />} />
            </Route>
          </Routes>
        </BrowserRouter>
      </UIProvider>
    </AppProvider>
  );
}
