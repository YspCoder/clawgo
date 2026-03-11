import React from 'react';

type InfoTileProps = {
  children: React.ReactNode;
  className?: string;
  contentClassName?: string;
  label: React.ReactNode;
  labelClassName?: string;
};

function joinClasses(...values: Array<string | undefined | false>) {
  return values.filter(Boolean).join(' ');
}

const InfoTile: React.FC<InfoTileProps> = ({
  children,
  className,
  contentClassName,
  label,
  labelClassName,
}) => {
  return (
    <div className={joinClasses('ui-subpanel p-4', className)}>
      <div className={joinClasses('ui-text-muted text-xs uppercase tracking-[0.25em]', labelClassName)}>
        {label}
      </div>
      <div className={joinClasses('ui-text-secondary mt-2 text-sm', contentClassName)}>{children}</div>
    </div>
  );
};

export default InfoTile;
