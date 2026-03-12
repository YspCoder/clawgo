import React from 'react';

function joinClasses(...values: Array<string | undefined | false>) {
  return values.filter(Boolean).join(' ');
}

type TextFieldProps = Omit<React.InputHTMLAttributes<HTMLInputElement>, 'className'> & {
  dense?: boolean;
  monospace?: boolean;
  className?: string;
};

type SelectFieldProps = Omit<React.SelectHTMLAttributes<HTMLSelectElement>, 'className'> & {
  dense?: boolean;
  className?: string;
};

type TextareaFieldProps = Omit<React.TextareaHTMLAttributes<HTMLTextAreaElement>, 'className'> & {
  dense?: boolean;
  monospace?: boolean;
  className?: string;
};

type SwitchFieldProps = Omit<React.InputHTMLAttributes<HTMLInputElement>, 'type' | 'className'> & {
  className?: string;
};

type FieldBlockProps = {
  label?: React.ReactNode;
  help?: React.ReactNode;
  meta?: React.ReactNode;
  className?: string;
  children: React.ReactNode;
};

type PanelFieldProps = FieldBlockProps & {
  dense?: boolean;
};

type SwitchCardFieldProps = {
  checked: boolean;
  className?: string;
  help?: React.ReactNode;
  label: React.ReactNode;
  onChange: (checked: boolean) => void;
};

type ToolbarSwitchFieldProps = {
  checked: boolean;
  className?: string;
  help?: React.ReactNode;
  label: React.ReactNode;
  onChange: (checked: boolean) => void;
};

type InlineSwitchFieldProps = {
  checked: boolean;
  className?: string;
  help?: React.ReactNode;
  label: React.ReactNode;
  onChange: (checked: boolean) => void;
};

export function TextField({ dense = false, monospace = false, className, ...props }: TextFieldProps) {
  return (
    <input
      {...props}
      className={joinClasses(
        'ui-input transition-all duration-200 focus:ring-2 focus:ring-indigo-500/50 focus:border-indigo-500 outline-none hover:border-zinc-700 w-full',
        dense ? 'rounded-lg px-2 py-1 text-xs' : 'rounded-xl px-3 py-2 text-sm',
        monospace && 'font-mono',
        className,
      )}
    />
  );
}

export function SelectField({ dense = false, className, children, ...props }: SelectFieldProps) {
  return (
    <select
      {...props}
      className={joinClasses(
        'ui-select transition-all duration-200 focus:ring-2 focus:ring-indigo-500/50 focus:border-indigo-500 outline-none hover:border-zinc-700 cursor-pointer w-full',
        dense ? 'rounded-lg px-2 py-1 text-xs' : 'rounded-xl px-3 py-2 text-sm',
        className,
      )}
    >
      {children}
    </select>
  );
}

export function TextareaField({ dense = false, monospace = false, className, ...props }: TextareaFieldProps) {
  return (
    <textarea
      {...props}
      className={joinClasses(
        'ui-textarea transition-all duration-200 focus:ring-2 focus:ring-indigo-500/50 focus:border-indigo-500 outline-none hover:border-zinc-700 w-full',
        dense ? 'rounded-lg px-2 py-1 text-xs' : 'rounded-xl px-3 py-2 text-sm',
        monospace && 'font-mono',
        className,
      )}
    />
  );
}

export function SwitchField({ className, ...props }: SwitchFieldProps) {
  return (
    <label className={joinClasses('ui-switch shrink-0', className)}>
      <input {...props} type="checkbox" className="ui-switch-input" />
      <span className="ui-switch-track">
        <span className="ui-switch-knob" />
      </span>
    </label>
  );
}

export function SwitchCardField({
  checked,
  className,
  help,
  label,
  onChange,
}: SwitchCardFieldProps) {
  return (
    <label className={joinClasses('flex items-center justify-between gap-3 py-2 cursor-pointer', className)}>
      <div className="min-w-0 space-y-0.5">
        <div className="ui-form-label text-sm font-semibold">{label}</div>
        {help ? <div className="ui-form-help text-xs leading-snug whitespace-pre-wrap">{help}</div> : null}
      </div>
      <SwitchField checked={checked} onChange={(e) => onChange(e.target.checked)} />
    </label>
  );
}

export function ToolbarSwitchField({
  checked,
  className,
  help,
  label,
  onChange,
}: ToolbarSwitchFieldProps) {
  return (
    <label className={joinClasses('ui-toolbar-checkbox cursor-pointer transition-colors duration-200 hover:bg-zinc-800/50', className)}>
      <div className="min-w-0 space-y-0.5">
        <div className="ui-text-primary text-xs font-semibold leading-tight">{label}</div>
        {help ? <div className="ui-form-help text-[10px] leading-tight">{help}</div> : null}
      </div>
      <SwitchField checked={checked} onChange={(e) => onChange(e.target.checked)} />
    </label>
  );
}

export function InlineSwitchField({
  checked,
  className,
  help,
  label,
  onChange,
}: InlineSwitchFieldProps) {
  return (
    <label className={joinClasses('flex items-center justify-between gap-3 rounded-xl border border-zinc-800/60 bg-zinc-900/40 px-3 py-2.5 cursor-pointer transition-all duration-200 hover:bg-zinc-800/60 hover:border-zinc-700 shadow-sm', className)}>
      <div className="min-w-0 space-y-0.5">
        <div className="ui-text-primary text-sm font-semibold leading-snug">{label}</div>
        {help ? <div className="ui-form-help text-xs text-zinc-400 leading-snug whitespace-pre-wrap">{help}</div> : null}
      </div>
      <SwitchField checked={checked} onChange={(e) => onChange(e.target.checked)} />
    </label>
  );
}

export function FieldBlock({ label, help, meta, className, children }: FieldBlockProps) {
  return (
    <div className={joinClasses('flex flex-col gap-1.5', className)}>
      {(label || help || meta) && (
        <div className="flex flex-col gap-1.5 mb-1.5">
          <div className="flex items-center justify-between gap-3">
            {label ? <div className="ui-form-label text-sm font-semibold">{label}</div> : null}
            {meta ? <div className="ui-form-help shrink-0 text-xs font-medium text-zinc-500">{meta}</div> : null}
          </div>
          {help ? <div className="ui-form-help text-xs text-zinc-400 leading-relaxed whitespace-pre-wrap">{help}</div> : null}
        </div>
      )}
      {children}
    </div>
  );
}

export function PanelField({ dense = false, className, ...props }: PanelFieldProps) {
  return (
    <FieldBlock
      {...props}
      className={joinClasses(
        'glass-panel rounded-xl border border-zinc-800/60 bg-zinc-900/40 shadow-sm',
        dense ? 'p-3 space-y-2' : 'p-4 space-y-3',
        className,
      )}
    />
  );
}
