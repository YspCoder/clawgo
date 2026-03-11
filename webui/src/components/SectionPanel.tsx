import React from 'react';

type SectionPanelProps = {
  actions?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
  headerClassName?: string;
  icon?: React.ReactNode;
  subtitle?: React.ReactNode;
  title?: React.ReactNode;
};

function joinClasses(...values: Array<string | undefined | false>) {
  return values.filter(Boolean).join(' ');
}

const SectionPanel: React.FC<SectionPanelProps> = ({
  actions,
  children,
  className,
  headerClassName,
  icon,
  subtitle,
  title,
}) => {
  return (
    <div className={joinClasses('brand-card rounded-[30px] border border-zinc-800/80 p-6', className)}>
      {title || subtitle || actions || icon ? (
        <div className={joinClasses('mb-5 flex items-center justify-between gap-3 flex-wrap', headerClassName)}>
          <div>
            {title ? (
              <div className="flex items-center gap-2 text-zinc-200">
                {icon}
                <h2 className="text-lg font-medium">{title}</h2>
              </div>
            ) : null}
            {subtitle ? <div className="mt-1 text-xs text-zinc-500">{subtitle}</div> : null}
          </div>
          {actions ? <div className="text-xs text-zinc-500">{actions}</div> : null}
        </div>
      ) : null}
      {children}
    </div>
  );
};

export default SectionPanel;
