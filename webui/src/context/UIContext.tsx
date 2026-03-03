import React, { createContext, useContext, useMemo, useState } from 'react';
import { AnimatePresence, motion } from 'motion/react';
import { useTranslation } from 'react-i18next';
import { GlobalDialog, DialogOptions } from '../components/GlobalDialog';

type UIContextType = {
  loading: boolean;
  showLoading: (text?: string) => void;
  hideLoading: () => void;
  notify: (opts: DialogOptions | string) => Promise<void>;
  confirmDialog: (opts: DialogOptions | string) => Promise<boolean>;
  openModal: (node: React.ReactNode, title?: string) => void;
  closeModal: () => void;
};

const UIContext = createContext<UIContextType | undefined>(undefined);

export const UIProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [loadingText, setLoadingText] = useState(t('loading'));
  const [dialog, setDialog] = useState<null | { kind: 'notice' | 'confirm'; options: DialogOptions; resolve: (v: any) => void }>(null);
  const [customModal, setCustomModal] = useState<null | { title?: string; node: React.ReactNode }>(null);

  const value = useMemo<UIContextType>(() => ({
    loading,
    showLoading: (text?: string) => {
      setLoadingText(text || t('loading'));
      setLoading(true);
    },
    hideLoading: () => setLoading(false),
    notify: (opts) => new Promise<void>((resolve) => {
      const options = typeof opts === 'string' ? { message: opts } : opts;
      setDialog({ kind: 'notice', options, resolve });
    }),
    confirmDialog: (opts) => new Promise<boolean>((resolve) => {
      const options = typeof opts === 'string' ? { message: opts } : opts;
      setDialog({ kind: 'confirm', options, resolve });
    }),
    openModal: (node, title) => setCustomModal({ node, title }),
    closeModal: () => setCustomModal(null),
  }), [loading, t]);

  const closeDialog = (result: boolean) => {
    if (!dialog) return;
    dialog.resolve(dialog.kind === 'notice' ? undefined : result);
    setDialog(null);
  };

  return (
    <UIContext.Provider value={value}>
      {children}

      <AnimatePresence>
        {loading && (
          <motion.div className="fixed inset-0 z-[120] bg-black/55 backdrop-blur-sm flex items-center justify-center"
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
        kind={(dialog?.kind || 'notice') as 'notice' | 'confirm'}
        options={dialog?.options || { message: '' }}
        onConfirm={() => closeDialog(true)}
        onCancel={() => closeDialog(false)}
      />

      <AnimatePresence>
        {customModal && (
          <motion.div className="fixed inset-0 z-[125] bg-black/60 backdrop-blur-sm flex items-center justify-center p-4"
            initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}>
            <motion.div className="w-full max-w-4xl rounded-2xl border border-zinc-700 bg-zinc-900 shadow-2xl overflow-hidden"
              initial={{ scale: 0.96 }} animate={{ scale: 1 }} exit={{ scale: 0.96 }}>
              <div className="px-5 py-3 border-b border-zinc-800 flex items-center justify-between">
                <h3 className="text-sm font-semibold text-zinc-100">{customModal.title || t('modal')}</h3>
                <button onClick={() => setCustomModal(null)} className="text-zinc-400 hover:text-zinc-200">✕</button>
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
