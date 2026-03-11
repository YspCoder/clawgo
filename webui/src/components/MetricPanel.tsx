import React from 'react';

type MetricPanelProps = {
  className?: string;
  icon?: React.ReactNode;
  iconContainerClassName?: string;
  layout?: 'stacked' | 'split';
  subtitle?: React.ReactNode;
  subtitleClassName?: string;
  title: React.ReactNode;
  titleClassName?: string;
  value: React.ReactNode;
  valueClassName?: string;
};

const BASE_CLASS_NAME = 'brand-card ui-border-subtle rounded-[28px] border p-5 min-h-[148px]';

const MetricPanel: React.FC<MetricPanelProps> = ({
  className,
  icon,
  iconContainerClassName,
  layout = 'stacked',
  subtitle,
  subtitleClassName,
  title,
  titleClassName,
  value,
  valueClassName,
}) => {
  if (layout === 'split') {
    return (
      <div className={`${BASE_CLASS_NAME} ${className || ''}`.trim()}>
        <div className="flex h-full items-start justify-between gap-3">
          <div className="flex min-h-full flex-1 flex-col">
            <div className={titleClassName || 'ui-text-muted text-[11px] uppercase tracking-widest'}>{title}</div>
            <div className={valueClassName || 'ui-text-primary mt-2 text-3xl font-semibold'}>{value}</div>
            {subtitle ? <div className={subtitleClassName || 'ui-text-muted mt-auto pt-4 text-xs'}>{subtitle}</div> : null}
          </div>
          {icon ? (
            <div className={iconContainerClassName || 'flex h-10 w-10 items-center justify-center rounded-xl bg-zinc-900/70'}>
              {icon}
            </div>
          ) : null}
        </div>
      </div>
    );
  }

  return (
    <div className={`${BASE_CLASS_NAME} ${className || ''}`.trim()}>
      <div className="ui-text-secondary flex items-center gap-2 mb-2">
        {icon}
        <div className={titleClassName || 'text-sm font-medium'}>{title}</div>
      </div>
      <div className={valueClassName || 'ui-text-primary text-3xl font-semibold'}>{value}</div>
      {subtitle ? <div className={subtitleClassName || 'ui-text-muted mt-2 text-xs'}>{subtitle}</div> : null}
    </div>
  );
};

export default MetricPanel;
