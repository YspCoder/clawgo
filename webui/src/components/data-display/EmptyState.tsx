import React from 'react';

type EmptyStateProps = {
  centered?: boolean;
  className?: string;
  dashed?: boolean;
  icon?: React.ReactNode;
  message: React.ReactNode;
  panel?: boolean;
  padded?: boolean;
  title?: React.ReactNode;
};

function joinClasses(...values: Array<string | undefined | false>) {
  return values.filter(Boolean).join(' ');
}

const EmptyState: React.FC<EmptyStateProps> = ({
  centered = false,
  className,
  dashed = false,
  icon,
  message,
  panel = false,
  padded = false,
  title,
}) => {
  return (
    <div
      className={joinClasses(
        'text-sm text-zinc-500',
        centered && 'text-center',
        padded && 'p-4',
        panel && 'brand-card border border-zinc-800/80 rounded-2xl',
        dashed && 'border-dashed',
        (icon || title) && 'flex flex-col items-center justify-center gap-2',
        className,
      )}
    >
      {icon ? <div className="text-zinc-500">{icon}</div> : null}
      {title ? <div className="text-lg font-medium text-zinc-300">{title}</div> : null}
      <div>{message}</div>
    </div>
  );
};

export default EmptyState;
