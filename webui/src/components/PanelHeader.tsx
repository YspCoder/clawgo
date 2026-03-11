import React from 'react';

type PanelHeaderProps = {
  className?: string;
  title: React.ReactNode;
};

function joinClasses(...values: Array<string | undefined | false>) {
  return values.filter(Boolean).join(' ');
}

const PanelHeader: React.FC<PanelHeaderProps> = ({ className, title }) => {
  return (
    <div className={joinClasses('px-3 py-2 border-b border-zinc-800 dark:border-zinc-700 text-xs text-zinc-400 uppercase tracking-wider', className)}>
      {title}
    </div>
  );
};

export default PanelHeader;
