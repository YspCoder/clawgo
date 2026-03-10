import React from 'react';
import { NavLink, useLocation } from 'react-router-dom';
import { LayoutDashboard, MessageSquare, Settings, Clock, Terminal, Zap, FolderOpen, ClipboardList, BrainCircuit, Hash, Bot, Boxes, PanelLeftClose, PanelLeftOpen, Plug, Smartphone, ChevronDown, Radio, MonitorSmartphone } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import NavItem from './NavItem';

const Sidebar: React.FC = () => {
  const { t } = useTranslation();
  const { token, setToken, sidebarOpen, sidebarCollapsed, setSidebarCollapsed, compiledChannels } = useAppContext();
  const location = useLocation();
  const [expandedSections, setExpandedSections] = React.useState<Record<string, boolean>>({
    main: true,
    agents: false,
    ops: false,
    config: false,
    knowledge: false,
    insights: false,
    channels: false,
  });

  const channelChildren = [
    { id: 'whatsapp', icon: <Smartphone className="w-4 h-4" />, label: t('whatsappBridge'), to: '/channels/whatsapp' },
    { id: 'telegram', icon: <Radio className="w-4 h-4" />, label: t('telegram'), to: '/channels/telegram' },
    { id: 'discord', icon: <MonitorSmartphone className="w-4 h-4" />, label: t('discord'), to: '/channels/discord' },
    { id: 'feishu', icon: <MonitorSmartphone className="w-4 h-4" />, label: t('feishu'), to: '/channels/feishu' },
    { id: 'qq', icon: <MonitorSmartphone className="w-4 h-4" />, label: t('qq'), to: '/channels/qq' },
    { id: 'dingtalk', icon: <MonitorSmartphone className="w-4 h-4" />, label: t('dingtalk'), to: '/channels/dingtalk' },
    { id: 'maixcam', icon: <MonitorSmartphone className="w-4 h-4" />, label: t('maixcam'), to: '/channels/maixcam' },
  ].filter((item) => compiledChannels.includes(item.id));

  const sections = [
    {
      id: 'main',
      title: t('sidebarMain'),
      items: [
        { icon: <LayoutDashboard className="w-5 h-5" />, label: t('dashboard'), to: '/' },
        { icon: <MessageSquare className="w-5 h-5" />, label: t('chat'), to: '/chat' },
      ],
    },
    {
      id: 'agents',
      title: t('sidebarAgents'),
      items: [
        { icon: <Boxes className="w-5 h-5" />, label: t('subagentsRuntime'), to: '/subagents' },
        { icon: <Bot className="w-5 h-5" />, label: t('subagentProfiles'), to: '/subagent-profiles' },
      ],
    },
    {
      id: 'ops',
      title: t('sidebarOps'),
      items: [
        { icon: <Terminal className="w-5 h-5" />, label: t('nodes'), to: '/nodes' },
        { icon: <FolderOpen className="w-5 h-5" />, label: t('nodeArtifacts'), to: '/node-artifacts' },
        { icon: <ClipboardList className="w-5 h-5" />, label: t('taskAudit'), to: '/task-audit' },
      ],
    },
    {
      id: 'config',
      title: t('sidebarConfig'),
      items: [
        { icon: <Settings className="w-5 h-5" />, label: t('config'), to: '/config' },
        {
          icon: <Smartphone className="w-5 h-5" />,
          label: t('channelsGroup'),
          childrenId: 'channels',
          children: channelChildren,
        },
        { icon: <Plug className="w-5 h-5" />, label: t('mcpServices'), to: '/mcp' },
        { icon: <Clock className="w-5 h-5" />, label: t('cronJobs'), to: '/cron' },
      ],
    },
    {
      id: 'knowledge',
      title: t('sidebarKnowledge'),
      items: [
        { icon: <FolderOpen className="w-5 h-5" />, label: t('memory'), to: '/memory' },
        { icon: <Zap className="w-5 h-5" />, label: t('skills'), to: '/skills' },
      ],
    },
    {
      id: 'insights',
      title: t('sidebarInsights'),
      items: [
        { icon: <Terminal className="w-5 h-5" />, label: t('logs'), to: '/logs' },
        { icon: <BrainCircuit className="w-5 h-5" />, label: t('ekg'), to: '/ekg' },
        { icon: <Hash className="w-5 h-5" />, label: t('logCodes'), to: '/log-codes' },
      ],
    },
  ];
  const normalizedSections = sections.map((sec) => ({
    ...sec,
    items: sec.items.filter((item: any) => !item.children || item.children.length > 0),
  }));

  const toggle = (id: string) => setExpandedSections((prev) => ({ ...prev, [id]: !prev[id] }));
  const isSubmenuActive = (items: Array<{ to: string }>) => items.some((item) => location.pathname === item.to);

  return (
    <aside className={`sidebar-shell fixed md:static inset-y-14 md:inset-y-16 left-0 z-40 ${sidebarCollapsed ? 'md:w-20' : 'md:w-64'} w-[86vw] max-w-72 border-r border-zinc-800 backdrop-blur-xl flex flex-col shrink-0 transform transition-all duration-200 ${sidebarOpen ? 'translate-x-0' : '-translate-x-full md:translate-x-0'}`}>
      <nav className={`flex-1 ${sidebarCollapsed ? 'px-2' : 'px-3'} py-3 space-y-2 overflow-y-auto`}>
        {normalizedSections.map((sec) => (
          <div key={sec.title} className={`sidebar-section rounded-2xl border border-zinc-800/60 ${sidebarCollapsed ? 'p-2' : 'p-2'}`}>
            {!sidebarCollapsed && (
              <button
                onClick={() => toggle(sec.id)}
                className="flex w-full items-center justify-between px-2 pb-1.5 text-[10px] font-bold uppercase tracking-widest text-zinc-500"
              >
                <span>{sec.title}</span>
                <ChevronDown className={`h-3.5 w-3.5 transition-transform ${expandedSections[sec.id] ? 'rotate-0' : '-rotate-90'}`} />
              </button>
            )}
            {(sidebarCollapsed || expandedSections[sec.id]) && (
              <div className="space-y-0.5">
                {sec.items.map((it: any) => {
                  if (!it.children) {
                    return <NavItem key={it.to} icon={it.icon} label={it.label} to={it.to} collapsed={sidebarCollapsed} />;
                  }
                  const submenuActive = isSubmenuActive(it.children);
                  const childrenOpen = sidebarCollapsed || expandedSections[it.childrenId];
                  return (
                    <div key={it.childrenId} className="space-y-1">
                      <button
                        onClick={() => !sidebarCollapsed && toggle(it.childrenId)}
                        className={`w-full flex items-center ${sidebarCollapsed ? 'justify-center' : 'gap-3'} px-3 py-2.5 rounded-lg text-sm font-medium transition-all duration-200 border ${
                          submenuActive ? 'nav-item-active text-indigo-700 border-indigo-500/30' : 'text-zinc-400 border-transparent'
                        }`}
                        title={sidebarCollapsed ? it.label : undefined}
                      >
                        <span className="shrink-0">{it.icon}</span>
                        {!sidebarCollapsed && (
                          <>
                            <span className="min-w-0 flex-1 truncate text-left">{it.label}</span>
                            <ChevronDown className={`h-4 w-4 transition-transform ${childrenOpen ? 'rotate-0' : '-rotate-90'}`} />
                          </>
                        )}
                      </button>
                      {childrenOpen && !sidebarCollapsed && (
                        <div className="ml-4 space-y-1 border-l border-zinc-800/70 pl-2">
                          {it.children.map((child: any) => (
                            <NavItem key={child.to} icon={child.icon} label={child.label} to={child.to} nested />
                          ))}
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        ))}
      </nav>

      {!sidebarCollapsed ? (
        <div className="p-3 border-t border-zinc-800 bg-zinc-900/20">
          <div className="text-[11px] font-medium text-zinc-500 mb-1 uppercase tracking-wider px-1">{t('gatewayToken')}</div>
          <input
            type="password"
            value={token}
            onChange={(e) => setToken(e.target.value)}
            placeholder={t('enterToken')}
            className="w-full bg-zinc-950/70 border border-zinc-800 rounded-xl px-3 py-2.5 text-sm focus:outline-none focus:border-indigo-500 focus:ring-2 focus:ring-indigo-500/20 transition-colors placeholder:text-zinc-600"
          />
        </div>
      ) : (
        <div className="hidden md:flex justify-center p-3 border-t border-zinc-800 bg-zinc-900/20">
          <div className="gateway-token-indicator w-2.5 h-2.5 rounded-full" title={t('gatewayToken')} />
        </div>
      )}
      <div className={`hidden md:flex border-t border-zinc-800 bg-zinc-900/20 ${sidebarCollapsed ? 'justify-center p-3' : 'justify-end p-3'}`}>
        <button
          onClick={() => setSidebarCollapsed((prev) => !prev)}
          className="flex h-11 w-11 items-center justify-center rounded-2xl border border-zinc-800 brand-card-subtle hover:bg-zinc-900/40 text-zinc-300 transition-colors"
          title={sidebarCollapsed ? t('expand') : t('collapse')}
        >
          {sidebarCollapsed ? <PanelLeftOpen className="w-4 h-4" /> : <PanelLeftClose className="w-4 h-4" />}
        </button>
      </div>
    </aside>
  );
};

export default Sidebar;
