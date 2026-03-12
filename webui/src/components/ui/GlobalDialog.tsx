import React from 'react';
import { AnimatePresence, motion } from 'motion/react';
import { useTranslation } from 'react-i18next';
import { Button } from './Button';
import { TextField, TextareaField } from './FormControls';

type DialogOptions = {
  title?: string;
  message: string;
  confirmText?: string;
  cancelText?: string;
  danger?: boolean;
  initialValue?: string;
  inputLabel?: string;
  inputPlaceholder?: string;
  monospace?: boolean;
  multiline?: boolean;
  wide?: boolean;
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
        <motion.div className="ui-overlay-strong fixed inset-0 z-[130] backdrop-blur-sm flex items-center justify-center p-4"
          initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}>
          <motion.div className={`brand-card w-full border border-zinc-700 shadow-2xl ${options.wide ? 'max-w-2xl' : 'max-w-md'}`}
            initial={{ scale: 0.95, y: 8 }} animate={{ scale: 1, y: 0 }} exit={{ scale: 0.95, y: 8 }}>
            <div className="px-5 py-4 border-b border-zinc-800 relative z-[1]">
              <h3 className="text-sm font-semibold text-zinc-100">{options.title || (kind === 'confirm' ? t('dialogPleaseConfirm') : kind === 'prompt' ? t('dialogInputTitle') : t('dialogNotice'))}</h3>
            </div>
            <div className="px-5 py-4 space-y-3 relative z-[1]">
              <div className="text-sm text-zinc-300 whitespace-pre-wrap break-all">{options.message}</div>
              {kind === 'prompt' && (
                <div className="space-y-2">
                  {options.inputLabel && <label className="text-xs text-zinc-400">{options.inputLabel}</label>}
                  {options.multiline ? (
                    <TextareaField
                      autoFocus
                      value={value}
                      onChange={(e) => setValue(e.target.value)}
                      placeholder={options.inputPlaceholder || t('dialogInputPlaceholder')}
                      monospace={options.monospace}
                      className="min-h-[96px] w-full text-zinc-100"
                    />
                  ) : (
                    <TextField
                      autoFocus
                      value={value}
                      onChange={(e) => setValue(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') {
                          onConfirm(value);
                        }
                      }}
                      placeholder={options.inputPlaceholder || t('dialogInputPlaceholder')}
                      monospace={options.monospace}
                      className="w-full text-zinc-100"
                    />
                  )}
                </div>
              )}
            </div>
            <div className="px-5 pb-5 flex items-center justify-end gap-2 relative z-[1]">
              {(kind === 'confirm' || kind === 'prompt') && (
                <Button onClick={onCancel} size="sm">{options.cancelText || t('cancel')}</Button>
              )}
              <Button onClick={() => onConfirm(kind === 'prompt' ? value : undefined)} variant={options.danger ? 'danger' : 'primary'} size="sm">
                {options.confirmText || t('dialogOk')}
              </Button>
            </div>
          </motion.div>
        </motion.div>
      )}
    </AnimatePresence>
  );
};

export type { DialogOptions };
