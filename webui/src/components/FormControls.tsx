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

type CheckboxFieldProps = Omit<React.InputHTMLAttributes<HTMLInputElement>, 'type' | 'className'> & {
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

export function TextField({ dense = false, monospace = false, className, ...props }: TextFieldProps) {
  return (
    <input
      {...props}
      className={joinClasses(
        'ui-input',
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
        'ui-select',
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
        'ui-textarea',
        dense ? 'rounded-lg px-2 py-1 text-xs' : 'rounded-xl px-3 py-2 text-sm',
        monospace && 'font-mono',
        className,
      )}
    />
  );
}

export function CheckboxField({ className, ...props }: CheckboxFieldProps) {
  return <input {...props} type="checkbox" className={joinClasses('ui-checkbox', className)} />;
}

export function FieldBlock({ label, help, meta, className, children }: FieldBlockProps) {
  return (
    <div className={joinClasses('space-y-1.5', className)}>
      {(label || help || meta) && (
        <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
          <div className="min-w-0 flex flex-wrap items-baseline gap-x-2 gap-y-1">
            {label ? <div className="ui-form-label">{label}</div> : null}
            {help ? <div className="ui-form-help text-[11px]">{help}</div> : null}
          </div>
          {meta ? <div className="ui-form-help text-[11px]">{meta}</div> : null}
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
        'rounded-xl border border-zinc-800 bg-zinc-900/30',
        dense ? 'p-2 space-y-2' : 'p-3 space-y-2',
        className,
      )}
    />
  );
}
