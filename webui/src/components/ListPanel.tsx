import React from 'react';

type ListPanelProps = {
  children: React.ReactNode;
  className?: string;
  header?: React.ReactNode;
};

function joinClasses(...values: Array<string | undefined | false>) {
  return values.filter(Boolean).join(' ');
}

const ListPanel: React.FC<ListPanelProps> = ({
  children,
  className,
  header,
}) => {
  return (
    <div className={joinClasses('brand-card ui-panel rounded-[28px] overflow-hidden flex flex-col min-h-0', className)}>
      {header}
      {children}
    </div>
  );
};

export default ListPanel;
