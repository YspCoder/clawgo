import React from 'react';
import { LayoutDashboard, MessageSquare, Settings, Clock, Server, Terminal, Globe, Zap } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import NavItem from './NavItem';

const Sidebar: React.FC = () => {
  const { t } = useTranslation();
  const { token, setToken } = useAppContext();

  return (
    <aside className="w-64 border-r border-zinc-800 bg-zinc-900/30 flex flex-col shrink-0">
      <nav className="flex-1 px-4 py-6 space-y-1 overflow-y-auto">
        <NavItem icon={<LayoutDashboard className="w-5 h-5" />} label={t('dashboard')} to="/" />
        <NavItem icon={<MessageSquare className="w-5 h-5" />} label={t('chat')} to="/chat" />
        <NavItem icon={<Terminal className="w-5 h-5" />} label={t('logs')} to="/logs" />
        <NavItem icon={<Zap className="w-5 h-5" />} label={t('skills')} to="/skills" />
        <div className="h-4" />
        <div className="text-[10px] font-bold text-zinc-600 uppercase tracking-widest px-4 mb-2">System</div>
        <NavItem icon={<Settings className="w-5 h-5" />} label={t('config')} to="/config" />
        <NavItem icon={<Clock className="w-5 h-5" />} label={t('cronJobs')} to="/cron" />
        <NavItem icon={<Server className="w-5 h-5" />} label={t('nodes')} to="/nodes" />
      </nav>

      <div className="p-4 border-t border-zinc-800 bg-zinc-900/50">
        <div className="text-xs font-medium text-zinc-500 mb-2 uppercase tracking-wider px-1">{t('gatewayToken')}</div>
        <input 
          type="password" 
          value={token} 
          onChange={(e) => setToken(e.target.value)} 
          placeholder={t('enterToken')}
          className="w-full bg-zinc-950 border border-zinc-800 rounded-lg px-3 py-2 text-sm focus:outline-none focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 transition-colors placeholder:text-zinc-600"
        />
      </div>
    </aside>
  );
};

export default Sidebar;
