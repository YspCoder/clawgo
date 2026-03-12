import React from 'react';

type FieldCardProps = {
  title?: React.ReactNode;
  hint?: React.ReactNode;
  className?: string;
  titleClassName?: string;
  hintClassName?: string;
  children: React.ReactNode;
};

function joinClasses(...values: Array<string | undefined | false>) {
  return values.filter(Boolean).join(' ');
}

const FieldCard: React.FC<FieldCardProps> = ({
  title,
  hint,
  className,
  titleClassName,
  hintClassName,
  children,
}) => {
  return (
    <div className={joinClasses('rounded-xl border border-zinc-800 bg-zinc-900/30 p-3 space-y-2', className)}>
      {title !== undefined && title !== null && (
        <div className={joinClasses('text-zinc-300', titleClassName)}>{title}</div>
      )}
      {hint !== undefined && hint !== null && (
        <div className={joinClasses('text-zinc-500', hintClassName)}>{hint}</div>
      )}
      {children}
    </div>
  );
};

export default FieldCard;
export type { FieldCardProps };
