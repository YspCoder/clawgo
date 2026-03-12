import React, { useMemo } from 'react';

type EKGDistributionCardProps = {
  data: Record<string, number>;
  title: string;
};

const EKGDistributionCard: React.FC<EKGDistributionCardProps> = ({
  data,
  title,
}) => {
  const entries = useMemo(() => (
    Object.entries(data).sort((a, b) => b[1] - a[1])
  ), [data]);
  const maxValue = entries.length > 0 ? Math.max(...entries.map(([, value]) => value)) : 0;

  return (
    <div className="brand-card ui-border-subtle rounded-2xl border p-5">
      <div className="ui-text-secondary mb-4 text-sm font-medium">{title}</div>
      <div className="space-y-3">
        {entries.length === 0 ? (
          <div className="ui-text-muted text-sm">-</div>
        ) : entries.map(([key, value]) => (
          <div key={key} className="space-y-1">
            <div className="flex items-center justify-between gap-3 text-xs">
              <div className="ui-text-secondary truncate">{key}</div>
              <div className="ui-text-muted shrink-0 font-mono">{value}</div>
            </div>
            <div className="ui-surface-muted h-2 rounded-full overflow-hidden">
              <div
                className="ekg-bar-fill h-full rounded-full"
                style={{ width: `${maxValue > 0 ? (value / maxValue) * 100 : 0}%` }}
              />
            </div>
          </div>
        ))}
      </div>
    </div>
  );
};

export default EKGDistributionCard;
