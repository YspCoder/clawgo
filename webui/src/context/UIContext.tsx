import React, { createContext, useContext, useMemo, useState } from 'react';
import { AnimatePresence, motion } from 'motion/react';

type ModalOptions = {
  title?: string;
  message: string;
  confirmText?: string;
  cancelText?: string;
  danger?: boolean;
};

type UIContextType = {
  loading: boolean;
  showLoading: (text?: string) => void;
  hideLoading: () => void;
  alert: (opts: ModalOptions | string) => Promise<void>;
  confirm: (opts: ModalOptions | string) => Promise<boolean>;
  openModal: (node: React.ReactNode, title?: string) => void;
  closeModal: () => void;
};

const UIContext = createContext<UIContextType | undefined>(undefined);

export const UIProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const [loading, setLoading] = useState(false);
  const [loadingText, setLoadingText] = useState('Loading...');
  const [dialog, setDialog] = useState<null | { kind: 'alert' | 'confirm'; options: ModalOptions; resolve: (v: any) => void }>(null);
  const [customModal, setCustomModal] = useState<null | { title?: string; node: React.ReactNode }>(null);

  const value = useMemo<UIContextType>(() => ({
    loading,
    showLoading: (text?: string) => {
      setLoadingText(text || 'Loading...');
      setLoading(true);
    },
    hideLoading: () => setLoading(false),
    alert: (opts) => new Promise<void>((resolve) => {
      const options = typeof opts === 'string' ? { message: opts } : opts;
      setDialog({ kind: 'alert', options, resolve });
    }),
    confirm: (opts) => new Promise<boolean>((resolve) => {
      const options = typeof opts === 'string' ? { message: opts } : opts;
      setDialog({ kind: 'confirm', options, resolve });
    }),
    openModal: (node, title) => setCustomModal({ node, title }),
    closeModal: () => setCustomModal(null),
  }), [loading]);

  const closeDialog = (result: boolean) => {
    if (!dialog) return;
    dialog.resolve(dialog.kind === 'alert' ? undefined : result);
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

      <AnimatePresence>
        {dialog && (
          <motion.div className="fixed inset-0 z-[130] bg-black/60 backdrop-blur-sm flex items-center justify-center p-4"
            initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}>
            <motion.div className="w-full max-w-md rounded-2xl border border-zinc-700 bg-zinc-900 shadow-2xl"
              initial={{ scale: 0.95, y: 8 }} animate={{ scale: 1, y: 0 }} exit={{ scale: 0.95, y: 8 }}>
              <div className="px-5 py-4 border-b border-zinc-800">
                <h3 className="text-sm font-semibold text-zinc-100">{dialog.options.title || (dialog.kind === 'confirm' ? 'Please confirm' : 'Notice')}</h3>
              </div>
              <div className="px-5 py-4 text-sm text-zinc-300 whitespace-pre-wrap">{dialog.options.message}</div>
              <div className="px-5 pb-5 flex items-center justify-end gap-2">
                {dialog.kind === 'confirm' && (
                  <button onClick={() => closeDialog(false)} className="px-3 py-1.5 rounded-lg bg-zinc-800 hover:bg-zinc-700 text-zinc-200 text-sm">{dialog.options.cancelText || 'Cancel'}</button>
                )}
                <button onClick={() => closeDialog(true)} className={`px-3 py-1.5 rounded-lg text-sm ${dialog.options.danger ? 'bg-red-600 hover:bg-red-500 text-white' : 'bg-indigo-600 hover:bg-indigo-500 text-white'}`}>
                  {dialog.options.confirmText || 'OK'}
                </button>
              </div>
            </motion.div>
          </motion.div>
        )}
      </AnimatePresence>

      <AnimatePresence>
        {customModal && (
          <motion.div className="fixed inset-0 z-[125] bg-black/60 backdrop-blur-sm flex items-center justify-center p-4"
            initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}>
            <motion.div className="w-full max-w-4xl rounded-2xl border border-zinc-700 bg-zinc-900 shadow-2xl overflow-hidden"
              initial={{ scale: 0.96 }} animate={{ scale: 1 }} exit={{ scale: 0.96 }}>
              <div className="px-5 py-3 border-b border-zinc-800 flex items-center justify-between">
                <h3 className="text-sm font-semibold text-zinc-100">{customModal.title || 'Modal'}</h3>
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
