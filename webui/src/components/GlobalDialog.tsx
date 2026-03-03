import React from 'react';
import { AnimatePresence, motion } from 'motion/react';
import { useTranslation } from 'react-i18next';

type DialogOptions = {
  title?: string;
  message: string;
  confirmText?: string;
  cancelText?: string;
  danger?: boolean;
};

export const GlobalDialog: React.FC<{
  open: boolean;
  kind: 'notice' | 'confirm';
  options: DialogOptions;
  onConfirm: () => void;
  onCancel: () => void;
}> = ({ open, kind, options, onConfirm, onCancel }) => {
  const { t } = useTranslation();
  return (
    <AnimatePresence>
      {open && (
        <motion.div className="fixed inset-0 z-[130] bg-black/60 backdrop-blur-sm flex items-center justify-center p-4"
          initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}>
          <motion.div className="w-full max-w-md rounded-2xl border border-zinc-700 bg-zinc-900 shadow-2xl"
            initial={{ scale: 0.95, y: 8 }} animate={{ scale: 1, y: 0 }} exit={{ scale: 0.95, y: 8 }}>
            <div className="px-5 py-4 border-b border-zinc-800">
              <h3 className="text-sm font-semibold text-zinc-100">{options.title || (kind === 'confirm' ? t('dialogPleaseConfirm') : t('dialogNotice'))}</h3>
            </div>
            <div className="px-5 py-4 text-sm text-zinc-300 whitespace-pre-wrap">{options.message}</div>
            <div className="px-5 pb-5 flex items-center justify-end gap-2">
              {kind === 'confirm' && (
                <button onClick={onCancel} className="px-3 py-1.5 rounded-lg bg-zinc-800 hover:bg-zinc-700 text-zinc-200 text-sm">{options.cancelText || t('cancel')}</button>
              )}
              <button onClick={onConfirm} className={`px-3 py-1.5 rounded-lg text-sm ${options.danger ? 'bg-red-600 hover:bg-red-500 text-white' : 'bg-indigo-600 hover:bg-indigo-500 text-white'}`}>
                {options.confirmText || t('dialogOk')}
              </button>
            </div>
          </motion.div>
        </motion.div>
      )}
    </AnimatePresence>
  );
};

export type { DialogOptions };
