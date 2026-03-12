import React from 'react';
import { Check } from 'lucide-react';

type SelectableListItemProps = {
  active?: boolean;
  children: React.ReactNode;
  className?: string;
  onClick: () => void;
};

function joinClasses(...values: Array<string | undefined | false>) {
  return values.filter(Boolean).join(' ');
}

const SelectableListItem: React.FC<SelectableListItemProps> = ({
  active = false,
  children,
  className,
  onClick,
}) => {
  return (
    <button
      onClick={onClick}
      className={joinClasses(
        'w-full text-left px-3 py-2 border-b border-zinc-800/60 transition-colors',
        active && 'bg-indigo-500/15',
        className,
      )}
    >
      <div className="flex items-center gap-3">
        <div className="min-w-0 flex-1">{children}</div>
        {active && (
          <span className="inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-indigo-500/15 text-indigo-300 self-center">
            <Check className="w-3.5 h-3.5" />
          </span>
        )}
      </div>
    </button>
  );
};

export default SelectableListItem;
