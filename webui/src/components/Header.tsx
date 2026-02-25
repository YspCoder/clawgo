import React from 'react';
import { Terminal, Globe } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';

const Header: React.FC = () => {
  const { t, i18n } = useTranslation();
  const { isGatewayOnline } = useAppContext();

  const toggleLang = () => {
    const nextLang = i18n.language === 'en' ? 'zh' : 'en';
    i18n.changeLanguage(nextLang);
  };

  return (
    <header className="h-16 border-b border-zinc-800 bg-zinc-900/50 flex items-center justify-between px-6 shrink-0 z-10">
      <div className="flex items-center gap-3">
        <div className="w-9 h-9 rounded-xl bg-indigo-500 flex items-center justify-center shadow-lg shadow-indigo-500/20">
          <Terminal className="w-5 h-5 text-white" />
        </div>
        <span className="font-semibold text-xl tracking-tight">ClawGo</span>
      </div>
      
      <div className="flex items-center gap-6">
        <div className="flex items-center gap-2.5 bg-zinc-900 border border-zinc-800 px-3 py-1.5 rounded-lg">
          <span className="text-sm font-medium text-zinc-400">{t('gatewayStatus')}:</span>
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
        
        <div className="h-5 w-px bg-zinc-800" />
        
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
