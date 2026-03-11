import React from 'react';

type ChannelSectionCardProps = {
  children: React.ReactNode;
  hint?: React.ReactNode;
  icon: React.ReactNode;
  title: React.ReactNode;
};

const ChannelSectionCard: React.FC<ChannelSectionCardProps> = ({
  children,
  hint,
  icon,
  title,
}) => {
  return (
    <section className="brand-card ui-panel rounded-[30px] p-6 space-y-5">
      <div className="ui-section-header">
        <div className="ui-subpanel flex h-11 w-11 shrink-0 items-center justify-center">
          {icon}
        </div>
        <div className="min-w-0">
          <div className="ui-text-primary text-lg font-semibold">{title}</div>
          {hint ? <p className="ui-text-muted mt-1 text-sm">{hint}</p> : null}
        </div>
      </div>
      {children}
    </section>
  );
};

export default ChannelSectionCard;
