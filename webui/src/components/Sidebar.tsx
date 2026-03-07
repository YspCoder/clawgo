import React from 'react';
import { LayoutDashboard, MessageSquare, Settings, Clock, Terminal, Zap, FolderOpen, ClipboardList, BrainCircuit, Hash, Bot, Boxes, PanelLeftClose, PanelLeftOpen } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import NavItem from './NavItem';

const Sidebar: React.FC = () => {
  const { t } = useTranslation();
  const { token, setToken, sidebarOpen, sidebarCollapsed, setSidebarCollapsed } = useAppContext();

  const sections = [
    {
      title: t('sidebarMain'),
      items: [
        { icon: <LayoutDashboard className="w-5 h-5" />, label: t('dashboard'), to: '/' },
        { icon: <MessageSquare className="w-5 h-5" />, label: t('chat'), to: '/chat' },
        { icon: <Boxes className="w-5 h-5" />, label: t('subagentsRuntime'), to: '/subagents' },
      ],
    },
    {
      title: t('sidebarRuntime'),
      items: [
        { icon: <ClipboardList className="w-5 h-5" />, label: t('taskAudit'), to: '/task-audit' },
        { icon: <Terminal className="w-5 h-5" />, label: t('logs'), to: '/logs' },
        { icon: <BrainCircuit className="w-5 h-5" />, label: t('ekg'), to: '/ekg' },
      ],
    },
    {
      title: t('sidebarConfig'),
      items: [
        { icon: <Settings className="w-5 h-5" />, label: t('config'), to: '/config' },
        { icon: <Bot className="w-5 h-5" />, label: t('subagentProfiles'), to: '/subagent-profiles' },
        { icon: <Clock className="w-5 h-5" />, label: t('cronJobs'), to: '/cron' },
      ],
    },
    {
      title: t('sidebarKnowledge'),
      items: [
        { icon: <FolderOpen className="w-5 h-5" />, label: t('memory'), to: '/memory' },
        { icon: <Zap className="w-5 h-5" />, label: t('skills'), to: '/skills' },
        { icon: <Hash className="w-5 h-5" />, label: t('logCodes'), to: '/log-codes' },
      ],
    },
  ];

  return (
    <aside className={`fixed md:static inset-y-14 md:inset-y-16 left-0 z-40 ${sidebarCollapsed ? 'md:w-20' : 'md:w-64'} w-[86vw] max-w-72 border-r border-zinc-800 bg-zinc-900/95 md:bg-zinc-900/40 backdrop-blur-sm flex flex-col shrink-0 transform transition-all duration-200 ${sidebarOpen ? 'translate-x-0' : '-translate-x-full md:translate-x-0'}`}>
      <nav className={`flex-1 ${sidebarCollapsed ? 'px-2' : 'px-3'} py-4 space-y-3 overflow-y-auto`}>
        {sections.map((sec) => (
          <div key={sec.title} className={`rounded-xl border border-zinc-800/60 bg-zinc-900/30 ${sidebarCollapsed ? 'p-2' : 'p-2'}`}>
            {!sidebarCollapsed && <div className="text-[10px] font-bold text-zinc-500 uppercase tracking-widest px-2 pb-2">{sec.title}</div>}
            <div className="space-y-1">
              {sec.items.map((it) => (
                <NavItem key={it.to} icon={it.icon} label={it.label} to={it.to} collapsed={sidebarCollapsed} />
              ))}
            </div>
          </div>
        ))}
      </nav>

      {!sidebarCollapsed ? (
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
      ) : (
        <div className="hidden md:flex justify-center p-3 border-t border-zinc-800 bg-zinc-900/60">
          <div className="w-2 h-2 rounded-full bg-zinc-600" title={t('gatewayToken')} />
        </div>
      )}
      <div className={`hidden md:flex border-t border-zinc-800 bg-zinc-900/60 ${sidebarCollapsed ? 'justify-center p-3' : 'p-3'}`}>
        <button
          onClick={() => setSidebarCollapsed((prev) => !prev)}
          className={`flex items-center ${sidebarCollapsed ? 'justify-center' : 'justify-between'} gap-3 rounded-xl border border-zinc-800 bg-zinc-950/70 hover:bg-zinc-900 text-zinc-300 transition-colors ${sidebarCollapsed ? 'w-11 h-11' : 'w-full px-3 py-2.5'}`}
          title={sidebarCollapsed ? t('expand') : t('collapse')}
        >
          {sidebarCollapsed ? <PanelLeftOpen className="w-4 h-4" /> : (
            <>
              <span className="text-sm font-medium">{t('collapse')}</span>
              <PanelLeftClose className="w-4 h-4 shrink-0" />
            </>
          )}
        </button>
      </div>
    </aside>
  );
};

export default Sidebar;
