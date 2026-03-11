import React, { createContext, useContext, useEffect, useMemo, useState } from 'react';
import { AnimatePresence, motion } from 'motion/react';
import { useTranslation } from 'react-i18next';
import { GlobalDialog, DialogOptions } from '../components/GlobalDialog';

type ThemeMode = 'light' | 'dark';

type UIContextType = {
  loading: boolean;
  theme: ThemeMode;
  setTheme: (theme: ThemeMode) => void;
  toggleTheme: () => void;
  showLoading: (text?: string) => void;
  hideLoading: () => void;
  withLoading: <T>(task: Promise<T> | (() => Promise<T>), text?: string) => Promise<T>;
  notify: (opts: DialogOptions | string) => Promise<void>;
  confirmDialog: (opts: DialogOptions | string) => Promise<boolean>;
  promptDialog: (opts: DialogOptions | string) => Promise<string | null>;
  openModal: (node: React.ReactNode, title?: string, onClose?: () => void) => void;
  closeModal: () => void;
};

const UIContext = createContext<UIContextType | undefined>(undefined);

function getInitialTheme(): ThemeMode {
  if (typeof window === 'undefined') return 'dark';
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

export const UIProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const { t } = useTranslation();
  const [loadingCount, setLoadingCount] = useState(0);
  const [loadingText, setLoadingText] = useState(t('loading'));
  const [theme, setTheme] = useState<ThemeMode>(getInitialTheme);
  const [dialog, setDialog] = useState<null | { kind: 'notice' | 'confirm' | 'prompt'; options: DialogOptions; resolve: (v: any) => void }>(null);
  const [customModal, setCustomModal] = useState<null | { title?: string; node: React.ReactNode; onClose?: () => void }>(null);
  const loading = loadingCount > 0;

  useEffect(() => {
    if (typeof document === 'undefined') return;
    const root = document.documentElement;
    root.classList.remove('theme-light', 'theme-dark');
    root.classList.add(theme === 'dark' ? 'theme-dark' : 'theme-light');
    root.style.colorScheme = theme;
  }, [theme]);

  useEffect(() => {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return;
    const media = window.matchMedia('(prefers-color-scheme: dark)');
    const applySystemTheme = (event?: MediaQueryList | MediaQueryListEvent) => {
      const matches = 'matches' in (event || media) ? (event || media).matches : media.matches;
      setTheme(matches ? 'dark' : 'light');
    };
    applySystemTheme(media);
    const onChange = (event: MediaQueryListEvent) => applySystemTheme(event);
    if (typeof media.addEventListener === 'function') {
      media.addEventListener('change', onChange);
      return () => media.removeEventListener('change', onChange);
    }
    media.addListener(onChange);
    return () => media.removeListener(onChange);
  }, []);

  const value = useMemo<UIContextType>(() => ({
    loading,
    theme,
    setTheme,
    toggleTheme: () => setTheme((current) => current === 'dark' ? 'light' : 'dark'),
    showLoading: (text?: string) => {
      setLoadingText(text || t('loading'));
      setLoadingCount((count) => count + 1);
    },
    hideLoading: () => setLoadingCount((count) => Math.max(0, count - 1)),
    withLoading: async (task, text) => {
      setLoadingText(text || t('loading'));
      setLoadingCount((count) => count + 1);
      try {
        return typeof task === 'function' ? await task() : await task;
      } finally {
        setLoadingCount((count) => Math.max(0, count - 1));
      }
    },
    notify: (opts) => new Promise<void>((resolve) => {
      const options = typeof opts === 'string' ? { message: opts } : opts;
      setDialog({ kind: 'notice', options, resolve });
    }),
    confirmDialog: (opts) => new Promise<boolean>((resolve) => {
      const options = typeof opts === 'string' ? { message: opts } : opts;
      setDialog({ kind: 'confirm', options, resolve });
    }),
    promptDialog: (opts) => new Promise<string | null>((resolve) => {
      const options = typeof opts === 'string' ? { message: opts } : opts;
      setDialog({ kind: 'prompt', options, resolve });
    }),
    openModal: (node, title, onClose) => setCustomModal({ node, title, onClose }),
    closeModal: () => setCustomModal((current) => {
      current?.onClose?.();
      return null;
    }),
  }), [loading, t, theme]);

  const closeDialog = (result?: boolean | string | null) => {
    if (!dialog) return;
    if (dialog.kind === 'notice') {
      dialog.resolve(undefined);
    } else if (dialog.kind === 'prompt') {
      dialog.resolve(typeof result === 'string' ? result : null);
    } else {
      dialog.resolve(Boolean(result));
    }
    setDialog(null);
  };

  return (
    <UIContext.Provider value={value}>
      {children}

      <AnimatePresence>
        {loading && (
          <motion.div className="ui-overlay-medium fixed inset-0 z-[120] backdrop-blur-sm flex items-center justify-center"
            initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}>
            <div className="px-6 py-5 rounded-2xl border border-zinc-700 bg-zinc-900/95 shadow-2xl min-w-[240px] text-center">
              <div className="mx-auto mb-3 h-8 w-8 border-2 border-zinc-600 border-t-indigo-400 rounded-full animate-spin" />
              <div className="text-sm text-zinc-200">{loadingText}</div>
            </div>
          </motion.div>
        )}
      </AnimatePresence>

      <GlobalDialog
        open={!!dialog}
        kind={(dialog?.kind || 'notice') as 'notice' | 'confirm' | 'prompt'}
        options={dialog?.options || { message: '' }}
        onConfirm={(value) => closeDialog(dialog?.kind === 'prompt' ? value || '' : true)}
        onCancel={() => closeDialog(dialog?.kind === 'prompt' ? null : false)}
      />

      <AnimatePresence>
        {customModal && (
          <motion.div className="ui-overlay-strong fixed inset-0 z-[125] backdrop-blur-sm flex items-center justify-center p-4"
            initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}>
            <button
              className="absolute inset-0"
              onClick={() => setCustomModal((current) => {
                current?.onClose?.();
                return null;
              })}
              aria-label={t('close')}
            />
            <motion.div className="relative w-full max-w-4xl rounded-2xl border border-zinc-700 bg-zinc-900 shadow-2xl overflow-hidden"
              initial={{ scale: 0.96 }} animate={{ scale: 1 }} exit={{ scale: 0.96 }}>
              <div className="px-5 py-3 border-b border-zinc-800 flex items-center justify-between">
                <h3 className="text-sm font-semibold text-zinc-100">{customModal.title || t('modal')}</h3>
                <button onClick={() => setCustomModal((current) => {
                  current?.onClose?.();
                  return null;
                })} className="text-zinc-400 hover:text-zinc-200">✕</button>
              </div>
              <div className="p-4 max-h-[80vh] overflow-auto">{customModal.node}</div>
            </motion.div>
          </motion.div>
        )}
      </AnimatePresence>
    </UIContext.Provider>
  );
};

export const useUI = () => {
  const ctx = useContext(UIContext);
  if (!ctx) throw new Error('useUI must be used within UIProvider');
  return ctx;
};
