import React from 'react';

type InfoBlockProps = {
  children: React.ReactNode;
  className?: string;
  contentClassName?: string;
  label: React.ReactNode;
  labelClassName?: string;
};

function joinClasses(...values: Array<string | undefined | false>) {
  return values.filter(Boolean).join(' ');
}

const InfoBlock: React.FC<InfoBlockProps> = ({
  children,
  className,
  contentClassName,
  label,
  labelClassName,
}) => {
  return (
    <div className={className}>
      <div className={joinClasses('text-zinc-500 text-xs mb-1', labelClassName)}>{label}</div>
      <div className={joinClasses('ui-code-panel p-2', contentClassName)}>{children}</div>
    </div>
  );
};

export default InfoBlock;
