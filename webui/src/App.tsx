import React from 'react';
import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { AppProvider } from './context/AppContext';
import Layout from './components/Layout';
import Dashboard from './pages/Dashboard';
import Chat from './pages/Chat';
import Config from './pages/Config';
import Cron from './pages/Cron';
import Nodes from './pages/Nodes';
import Logs from './pages/Logs';
import Skills from './pages/Skills';
import Memory from './pages/Memory';

export default function App() {
  return (
    <AppProvider>
      <BrowserRouter basename="/webui">
        <Routes>
          <Route path="/" element={<Layout />}>
            <Route index element={<Dashboard />} />
            <Route path="chat" element={<Chat />} />
            <Route path="logs" element={<Logs />} />
            <Route path="skills" element={<Skills />} />
            <Route path="config" element={<Config />} />
            <Route path="cron" element={<Cron />} />
            <Route path="nodes" element={<Nodes />} />
            <Route path="memory" element={<Memory />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </AppProvider>
  );
}
