import React from 'react';
import { Terminal, Globe, Menu } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';

const Header: React.FC = () => {
  const { t, i18n } = useTranslation();
  const { isGatewayOnline, setSidebarOpen } = useAppContext();

  const toggleLang = () => {
    const nextLang = i18n.language === 'en' ? 'zh' : 'en';
    i18n.changeLanguage(nextLang);
  };

  return (
    <header className="h-14 md:h-16 border-b border-zinc-800 bg-zinc-900/70 flex items-center justify-between px-3 md:px-6 shrink-0 z-10">
      <div className="flex items-center gap-2 md:gap-3 min-w-0">
        <button className="md:hidden p-2 rounded-lg hover:bg-zinc-800 text-zinc-300" onClick={() => setSidebarOpen(true)}>
          <Menu className="w-5 h-5" />
        </button>
        <div className="w-8 h-8 md:w-9 md:h-9 rounded-xl bg-indigo-500 flex items-center justify-center shadow-lg shadow-indigo-500/20 shrink-0">
          <Terminal className="w-4 h-4 md:w-5 md:h-5 text-white" />
        </div>
        <span className="hidden md:inline font-semibold text-lg md:text-xl tracking-tight truncate">{t('appName')}</span>
      </div>
      
      <div className="flex items-center gap-2 md:gap-6">
        <div className="flex items-center gap-1.5 md:gap-2.5 bg-zinc-900 border border-zinc-800 px-2 md:px-3 py-1 rounded-lg max-w-[140px] md:max-w-none overflow-hidden">
          <span className="hidden md:inline text-sm font-medium text-zinc-400">{t('gatewayStatus')}:</span>
          {isGatewayOnline ? (
            <div className="flex items-center gap-1.5 bg-emerald-500/10 text-emerald-400 px-2.5 py-0.5 rounded-md text-xs font-semibold border border-emerald-500/20">
              <div className="w-1.5 h-1.5 rounded-full bg-emerald-500 shadow-[0_0_8px_rgba(16,185,129,0.8)]" />
              {t('online')}
            </div>
          ) : (
            <div className="flex items-center gap-1.5 bg-red-500/10 text-red-400 px-2.5 py-0.5 rounded-md text-xs font-semibold border border-red-500/20">
              <div className="w-1.5 h-1.5 rounded-full bg-red-500 shadow-[0_0_8px_rgba(239,68,68,0.8)]" />
              {t('offline')}
            </div>
          )}
        </div>
        
        <div className="hidden md:block h-5 w-px bg-zinc-800" />
        
        <button 
          onClick={toggleLang}
          className="flex items-center gap-2 text-sm font-medium text-zinc-400 hover:text-zinc-200 transition-colors bg-zinc-900 hover:bg-zinc-800 border border-zinc-800 px-3 py-1.5 rounded-lg"
        >
          <Globe className="w-4 h-4" />
          {i18n.language === 'en' ? '中文' : 'English'}
        </button>
      </div>
    </header>
  );
};

export default Header;
