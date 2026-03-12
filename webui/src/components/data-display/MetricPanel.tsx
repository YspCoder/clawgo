import React from 'react';
import { motion } from 'motion/react';

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

const BASE_CLASS_NAME = 'brand-card hover-lift glow-effect ui-border-subtle rounded-[28px] border p-5 min-h-[148px]';

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
      <motion.div 
        whileHover={{ scale: 1.015 }}
        transition={{ type: "spring", stiffness: 300, damping: 20 }}
        className={`${BASE_CLASS_NAME} ${className || ''}`.trim()}
      >
        <div className="flex h-full items-start justify-between gap-3 relative z-10">
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
      </motion.div>
    );
  }

  return (
    <motion.div 
      whileHover={{ scale: 1.015 }}
      transition={{ type: "spring", stiffness: 300, damping: 20 }}
      className={`${BASE_CLASS_NAME} ${className || ''}`.trim()}
    >
      <div className="relative z-10">
        <div className="ui-text-secondary flex items-center gap-2 mb-2">
          {icon}
          <div className={titleClassName || 'text-sm font-medium'}>{title}</div>
        </div>
        <div className={valueClassName || 'ui-text-primary text-3xl font-semibold'}>{value}</div>
        {subtitle ? <div className={subtitleClassName || 'ui-text-muted mt-2 text-xs'}>{subtitle}</div> : null}
      </div>
    </motion.div>
  );
};

export default React.memo(MetricPanel);
