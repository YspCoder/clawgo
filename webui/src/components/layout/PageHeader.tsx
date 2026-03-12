import React from 'react';

type PageHeaderProps = {
  actions?: React.ReactNode;
  className?: string;
  subtitle?: React.ReactNode;
  title: React.ReactNode;
  titleClassName?: string;
};

function joinClasses(...values: Array<string | undefined | false>) {
  return values.filter(Boolean).join(' ');
}

const PageHeader: React.FC<PageHeaderProps> = ({
  actions,
  className,
  subtitle,
  title,
  titleClassName,
}) => {
  return (
    <div className={joinClasses('flex items-start justify-between gap-3 flex-wrap', className)}>
      <div className="min-w-0">
        <h1 className={joinClasses('text-2xl font-semibold tracking-tight', titleClassName)}>{title}</h1>
        {subtitle ? <div className="text-sm text-zinc-500 mt-1">{subtitle}</div> : null}
      </div>
      {actions ? <div className="flex items-center gap-3 flex-wrap">{actions}</div> : null}
    </div>
  );
};

export default PageHeader;
