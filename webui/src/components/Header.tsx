import React from 'react';
import { Github, Moon, RefreshCw, SunMedium, Terminal } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import { useUI } from '../context/UIContext';
import { FixedButton, FixedLinkButton } from './Button';

const REPO_URL = 'https://github.com/YspCoder/clawgo';

function normalizeVersion(value: string) {
  return String(value || '').trim().replace(/^v/i, '');
}

const Header: React.FC = () => {
  const { t, i18n } = useTranslation();
  const { isGatewayOnline, sidebarCollapsed, gatewayVersion, webuiVersion } = useAppContext();
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
    <header className="app-header ui-border-subtle h-14 md:h-16 border-b flex items-center justify-between px-3 md:px-6 shrink-0 z-10">
      <div className="flex items-center gap-2 md:gap-3 min-w-0">
        <div className="brand-badge hidden md:flex h-9 w-9 items-center justify-center rounded-xl shadow-lg shrink-0">
          <Terminal className="h-5 w-5 text-white" />
        </div>
        <div className="brand-badge flex h-8 w-8 items-center justify-center rounded-xl shadow-lg shrink-0 md:hidden">
          <Terminal className="h-4 w-4 text-white" />
        </div>
        {!sidebarCollapsed && (
          <span className="hidden md:inline font-semibold text-lg md:text-xl tracking-tight truncate">{t('appName')}</span>
        )}
        <span className="md:hidden font-semibold text-lg tracking-tight truncate">{t('appName')}</span>
      </div>
      
      <div className="flex items-center gap-2 md:gap-6">
        <div className="ui-toolbar-chip flex items-center gap-1.5 md:gap-2.5 px-2 md:px-3 py-1 rounded-lg max-w-[140px] md:max-w-none overflow-hidden">
          <span className="ui-text-muted hidden md:inline text-sm font-medium">{t('gatewayStatus')}:</span>
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
        
        <div className="ui-border-subtle hidden md:block h-5 w-px bg-transparent border-l" />

        <FixedLinkButton href={REPO_URL} target="_blank" rel="noreferrer" label={t('githubRepo')}>
          <Github className="w-4 h-4" />
        </FixedLinkButton>

        <FixedButton onClick={checkVersion} disabled={checkingVersion} label={t('checkVersion')}>
          <RefreshCw className={`w-4 h-4 ${checkingVersion ? 'animate-spin' : ''}`} />
        </FixedButton>

        <FixedButton onClick={toggleTheme} label={theme === 'dark' ? t('themeLight') : t('themeDark')}>
          {theme === 'dark' ? <SunMedium className="w-4 h-4" /> : <Moon className="w-4 h-4" />}
        </FixedButton>
        
        <FixedButton onClick={toggleLang} shape="square" label={i18n.language === 'en' ? t('languageZh') : t('languageEn')}>
          {i18n.language === 'en' ? '中' : 'EN'}
        </FixedButton>
      </div>
    </header>
  );
};

export default Header;
