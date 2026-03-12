import React from 'react';
import SelectableListItem from './SelectableListItem';

type SummaryListItemProps = {
  active?: boolean;
  badges?: React.ReactNode;
  className?: string;
  meta?: React.ReactNode;
  onClick?: () => void;
  subtitle?: React.ReactNode;
  title: React.ReactNode;
  trailing?: React.ReactNode;
};

const SummaryListItem: React.FC<SummaryListItemProps> = ({
  active,
  badges,
  className,
  meta,
  onClick,
  subtitle,
  title,
  trailing,
}) => {
  return (
    <SelectableListItem active={active} className={className} onClick={onClick}>
      <div className="flex items-start gap-3">
        <div className="min-w-0 flex-1">
          <div className="text-sm font-medium text-zinc-100 truncate">{title}</div>
          {subtitle ? <div className="text-xs text-zinc-400 truncate">{subtitle}</div> : null}
          {meta ? <div className="text-[11px] text-zinc-500 truncate">{meta}</div> : null}
          {badges ? <div className="flex flex-wrap gap-1 mt-2">{badges}</div> : null}
        </div>
        {trailing}
      </div>
    </SelectableListItem>
  );
};

export default SummaryListItem;
