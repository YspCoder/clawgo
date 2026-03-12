import React from 'react';

type ToolbarRowProps = {
  children: React.ReactNode;
  className?: string;
};

function joinClasses(...values: Array<string | undefined | false>) {
  return values.filter(Boolean).join(' ');
}

const ToolbarRow: React.FC<ToolbarRowProps> = ({ children, className }) => {
  return (
    <div className={joinClasses('flex items-center gap-2 flex-wrap', className)}>
      {children}
    </div>
  );
};

export default ToolbarRow;
