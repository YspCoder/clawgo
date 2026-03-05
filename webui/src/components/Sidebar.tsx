import React from 'react';
import { LayoutDashboard, MessageSquare, Settings, Clock, Server, Terminal, Zap, FolderOpen, ClipboardList, BrainCircuit, Hash, Bot, Workflow, Boxes } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import NavItem from './NavItem';

const Sidebar: React.FC = () => {
  const { t } = useTranslation();
  const { token, setToken, sidebarOpen } = useAppContext();

  const sections = [
    {
      title: t('sidebarCore'),
      items: [
        { icon: <LayoutDashboard className="w-5 h-5" />, label: t('dashboard'), to: '/' },
        { icon: <MessageSquare className="w-5 h-5" />, label: t('chat'), to: '/chat' },
        { icon: <Terminal className="w-5 h-5" />, label: t('logs'), to: '/logs' },
        { icon: <Hash className="w-5 h-5" />, label: t('logCodes'), to: '/log-codes' },
        { icon: <Zap className="w-5 h-5" />, label: t('skills'), to: '/skills' },
      ],
    },
    {
      title: t('sidebarSystem'),
      items: [
        { icon: <Settings className="w-5 h-5" />, label: t('config'), to: '/config' },
        { icon: <Clock className="w-5 h-5" />, label: t('cronJobs'), to: '/cron' },
        { icon: <Server className="w-5 h-5" />, label: t('nodes'), to: '/nodes' },
        { icon: <FolderOpen className="w-5 h-5" />, label: t('memory'), to: '/memory' },
        { icon: <Bot className="w-5 h-5" />, label: t('subagentProfiles'), to: '/subagent-profiles' },
        { icon: <Boxes className="w-5 h-5" />, label: t('subagentsRuntime'), to: '/subagents' },
        { icon: <Workflow className="w-5 h-5" />, label: t('pipelines'), to: '/pipelines' },
      ],
    },
    {
      title: t('sidebarOps'),
      items: [
        { icon: <ClipboardList className="w-5 h-5" />, label: t('taskAudit'), to: '/task-audit' },
      ],
    },
    {
      title: t('sidebarInsights'),
      items: [
        { icon: <BrainCircuit className="w-5 h-5" />, label: t('ekg'), to: '/ekg' },
      ],
    },
  ];

  return (
    <aside className={`fixed md:static inset-y-16 left-0 z-40 w-64 border-r border-zinc-800 bg-zinc-900/95 md:bg-zinc-900/40 backdrop-blur-sm flex flex-col shrink-0 transform transition-transform duration-200 ${sidebarOpen ? 'translate-x-0' : '-translate-x-full md:translate-x-0'}`}>
      <nav className="flex-1 px-3 py-4 space-y-3 overflow-y-auto">
        {sections.map((sec) => (
          <div key={sec.title} className="rounded-xl border border-zinc-800/60 bg-zinc-900/30 p-2">
            <div className="text-[10px] font-bold text-zinc-500 uppercase tracking-widest px-2 pb-2">{sec.title}</div>
            <div className="space-y-1">
              {sec.items.map((it) => (
                <NavItem key={it.to} icon={it.icon} label={it.label} to={it.to} />
              ))}
            </div>
          </div>
        ))}
      </nav>

      <div className="p-3 border-t border-zinc-800 bg-zinc-900/60">
        <div className="text-[11px] font-medium text-zinc-500 mb-1 uppercase tracking-wider px-1">{t('gatewayToken')}</div>
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
