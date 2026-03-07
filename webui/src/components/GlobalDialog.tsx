import React from 'react';
import { AnimatePresence, motion } from 'motion/react';
import { useTranslation } from 'react-i18next';

type DialogOptions = {
  title?: string;
  message: string;
  confirmText?: string;
  cancelText?: string;
  danger?: boolean;
  initialValue?: string;
  inputLabel?: string;
  inputPlaceholder?: string;
};

export const GlobalDialog: React.FC<{
  open: boolean;
  kind: 'notice' | 'confirm' | 'prompt';
  options: DialogOptions;
  onConfirm: (value?: string) => void;
  onCancel: () => void;
}> = ({ open, kind, options, onConfirm, onCancel }) => {
  const { t } = useTranslation();
  const [value, setValue] = React.useState(options.initialValue || '');

  React.useEffect(() => {
    if (open) {
      setValue(options.initialValue || '');
    }
  }, [open, options.initialValue]);

  return (
    <AnimatePresence>
      {open && (
        <motion.div className="fixed inset-0 z-[130] bg-black/60 backdrop-blur-sm flex items-center justify-center p-4"
          initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}>
          <motion.div className="brand-card w-full max-w-md border border-zinc-700 shadow-2xl"
            initial={{ scale: 0.95, y: 8 }} animate={{ scale: 1, y: 0 }} exit={{ scale: 0.95, y: 8 }}>
            <div className="px-5 py-4 border-b border-zinc-800 relative z-[1]">
              <h3 className="text-sm font-semibold text-zinc-100">{options.title || (kind === 'confirm' ? t('dialogPleaseConfirm') : kind === 'prompt' ? t('dialogInputTitle') : t('dialogNotice'))}</h3>
            </div>
            <div className="px-5 py-4 space-y-3 relative z-[1]">
              <div className="text-sm text-zinc-300 whitespace-pre-wrap">{options.message}</div>
              {kind === 'prompt' && (
                <div className="space-y-2">
                  {options.inputLabel && <label className="text-xs text-zinc-400">{options.inputLabel}</label>}
                  <input
                    autoFocus
                    value={value}
                    onChange={(e) => setValue(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') {
                        onConfirm(value);
                      }
                    }}
                    placeholder={options.inputPlaceholder || t('dialogInputPlaceholder')}
                    className="w-full px-3 py-2 rounded-xl bg-zinc-950/75 border border-zinc-800 text-sm text-zinc-100 focus:outline-none focus:border-indigo-500 focus:ring-2 focus:ring-indigo-500/20"
                  />
                </div>
              )}
            </div>
            <div className="px-5 pb-5 flex items-center justify-end gap-2 relative z-[1]">
              {(kind === 'confirm' || kind === 'prompt') && (
                <button onClick={onCancel} className="px-3 py-1.5 rounded-xl bg-zinc-800 hover:bg-zinc-700 text-zinc-200 text-sm">{options.cancelText || t('cancel')}</button>
              )}
              <button onClick={() => onConfirm(kind === 'prompt' ? value : undefined)} className={`px-3 py-1.5 rounded-xl text-sm ${options.danger ? 'bg-red-600 hover:bg-red-500 text-white' : 'brand-button text-white'}`}>
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
