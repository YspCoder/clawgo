import React from 'react';

type FormFieldProps = {
  label?: React.ReactNode;
  help?: React.ReactNode;
  className?: string;
  labelClassName?: string;
  helpClassName?: string;
  children: React.ReactNode;
};

function joinClasses(...values: Array<string | undefined | false>) {
  return values.filter(Boolean).join(' ');
}

const FormField: React.FC<FormFieldProps> = ({
  label,
  help,
  className,
  labelClassName,
  helpClassName,
  children,
}) => {
  return (
    <label className={joinClasses('block space-y-1.5', className)}>
      {label !== undefined && label !== null && (
        <div className={joinClasses('ui-form-label', labelClassName)}>{label}</div>
      )}
      {help !== undefined && help !== null && (
        <div className={joinClasses('ui-form-help', helpClassName)}>{help}</div>
      )}
      {children}
    </label>
  );
};

export default FormField;
export type { FormFieldProps };
