import React from 'react';

type SectionHeaderProps = {
  actions?: React.ReactNode;
  className?: string;
  meta?: React.ReactNode;
  subtitle?: React.ReactNode;
  title: React.ReactNode;
};

function joinClasses(...values: Array<string | undefined | false>) {
  return values.filter(Boolean).join(' ');
}

const SectionHeader: React.FC<SectionHeaderProps> = ({
  actions,
  className,
  meta,
  subtitle,
  title,
}) => {
  return (
    <div className={joinClasses('flex items-start justify-between gap-3 flex-wrap', className)}>
      <div className="min-w-0">
        <div className="text-sm font-semibold text-zinc-200">{title}</div>
        {subtitle ? <div className="text-xs text-zinc-500 mt-1">{subtitle}</div> : null}
      </div>
      {actions ? <div className="flex items-center gap-2 flex-wrap shrink-0">{actions}</div> : meta ? <div className="text-xs text-zinc-500 shrink-0">{meta}</div> : null}
    </div>
  );
};

export default SectionHeader;
