import React from 'react';
import { Terminal, Globe, Github, Menu, Moon, RefreshCw, SunMedium } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import { useUI } from '../context/UIContext';

const REPO_URL = 'https://github.com/YspCoder/clawgo';

function normalizeVersion(value: string) {
  return String(value || '').trim().replace(/^v/i, '');
}

const Header: React.FC = () => {
  const { t, i18n } = useTranslation();
  const { isGatewayOnline, setSidebarOpen, sidebarCollapsed, gatewayVersion, webuiVersion } = useAppContext();
  const { theme, toggleTheme, notify } = useUI();
  const [checkingVersion, setCheckingVersion] = React.useState(false);

  const toggleLang = () => {
    const nextLang = i18n.language === 'en' ? 'zh' : 'en';
    i18n.changeLanguage(nextLang);
  };

  const checkVersion = async () => {
    setCheckingVersion(true);
    try {
      const response = await fetch('https://api.github.com/repos/YspCoder/clawgo/releases/latest', {
        headers: { Accept: 'application/vnd.github+json' },
      });
      if (!response.ok) {
        throw new Error(`GitHub API ${response.status}`);
      }
      const data = await response.json();
      const latest = normalizeVersion(data?.tag_name || '');
      const currentGateway = normalizeVersion(gatewayVersion);
      const currentWebUI = normalizeVersion(webuiVersion);
      const isCurrent = latest && latest === currentGateway && latest === currentWebUI;
      await notify({
        title: isCurrent ? t('versionCheckUpToDateTitle') : t('versionCheckUpdateTitle'),
        message: isCurrent
          ? t('versionCheckUpToDateMessage', { version: latest || '-' })
          : t('versionCheckUpdateMessage', {
              latest: latest || '-',
              gateway: currentGateway || '-',
              webui: currentWebUI || '-',
            }),
      });
    } catch (error) {
      await notify({
        title: t('versionCheckFailedTitle'),
        message: t('versionCheckFailedMessage', { error: error instanceof Error ? error.message : String(error) }),
      });
    } finally {
      setCheckingVersion(false);
    }
  };

  return (
    <header className="app-header h-14 md:h-16 border-b border-zinc-800 flex items-center justify-between px-3 md:px-6 shrink-0 z-10">
      <div className="flex items-center gap-2 md:gap-3 min-w-0">
        <button className="md:hidden p-2 rounded-lg hover:bg-zinc-800 text-zinc-300" onClick={() => setSidebarOpen(true)}>
          <Menu className="w-5 h-5" />
        </button>
        <div className="hidden md:flex items-center gap-3 rounded-xl px-2 py-1.5 min-w-0">
          <div className="brand-badge w-9 h-9 rounded-xl flex items-center justify-center shadow-lg shrink-0">
            <Terminal className="w-5 h-5 text-zinc-950" />
          </div>
          {!sidebarCollapsed && (
            <span className="font-semibold text-lg md:text-xl tracking-tight truncate">{t('appName')}</span>
          )}
        </div>
        <div className="brand-badge md:hidden w-8 h-8 rounded-xl flex items-center justify-center shadow-lg shrink-0">
          <Terminal className="w-4 h-4 text-zinc-950" />
        </div>
        <span className="md:hidden font-semibold text-lg tracking-tight truncate">{t('appName')}</span>
      </div>
      
      <div className="flex items-center gap-2 md:gap-6">
        <div className="flex items-center gap-1.5 md:gap-2.5 bg-zinc-900 border border-zinc-800 px-2 md:px-3 py-1 rounded-lg max-w-[140px] md:max-w-none overflow-hidden">
          <span className="hidden md:inline text-sm font-medium text-zinc-400">{t('gatewayStatus')}:</span>
          {isGatewayOnline ? (
            <div className="status-pill-online flex items-center gap-1.5 px-2.5 py-0.5 rounded-md text-xs font-semibold border">
              <div className="status-dot-online w-1.5 h-1.5 rounded-full" />
              {t('online')}
            </div>
          ) : (
            <div className="status-pill-offline flex items-center gap-1.5 px-2.5 py-0.5 rounded-md text-xs font-semibold border">
              <div className="status-dot-offline w-1.5 h-1.5 rounded-full" />
              {t('offline')}
            </div>
          )}
        </div>
        
        <div className="hidden md:block h-5 w-px bg-zinc-800" />

        <a
          href={REPO_URL}
          target="_blank"
          rel="noreferrer"
          className="inline-flex h-9 w-9 items-center justify-center text-sm font-medium text-zinc-400 hover:text-zinc-200 transition-colors bg-zinc-900 hover:bg-zinc-800 border border-zinc-800 rounded-lg"
          title={t('githubRepo')}
        >
          <Github className="w-4 h-4" />
        </a>

        <button
          onClick={checkVersion}
          disabled={checkingVersion}
          className="inline-flex h-9 w-9 items-center justify-center text-sm font-medium text-zinc-400 hover:text-zinc-200 transition-colors bg-zinc-900 hover:bg-zinc-800 border border-zinc-800 rounded-lg disabled:opacity-60"
          title={t('checkVersion')}
        >
          <RefreshCw className={`w-4 h-4 ${checkingVersion ? 'animate-spin' : ''}`} />
        </button>

        <button
          onClick={toggleTheme}
          className="inline-flex h-9 w-9 items-center justify-center text-sm font-medium text-zinc-400 hover:text-zinc-200 transition-colors bg-zinc-900 hover:bg-zinc-800 border border-zinc-800 rounded-lg"
          title={theme === 'dark' ? t('themeLight') : t('themeDark')}
        >
          {theme === 'dark' ? <SunMedium className="w-4 h-4" /> : <Moon className="w-4 h-4" />}
        </button>
        
        <button 
          onClick={toggleLang}
          className="flex items-center gap-2 text-sm font-medium text-zinc-400 hover:text-zinc-200 transition-colors bg-zinc-900 hover:bg-zinc-800 border border-zinc-800 px-3 py-1.5 rounded-lg"
        >
          <Globe className="w-4 h-4" />
          {i18n.language === 'en' ? t('languageZh') : t('languageEn')}
        </button>
      </div>
    </header>
  );
};

export default Header;
