import React from 'react';

type EKGKV = { key?: string; score?: number; count?: number };

type EKGRankingCardProps = {
  items: EKGKV[];
  title: string;
  valueMode: 'score' | 'count';
};

const EKGRankingCard: React.FC<EKGRankingCardProps> = ({
  items,
  title,
  valueMode,
}) => {
  return (
    <div className="brand-card ui-border-subtle rounded-2xl border p-5">
      <div className="ui-text-secondary mb-4 text-sm font-medium">{title}</div>
      <div className="space-y-2">
        {items.length === 0 ? (
          <div className="ui-text-muted text-sm">-</div>
        ) : items.map((item, index) => (
          <div key={`${item.key || '-'}-${index}`} className="ui-border-subtle ui-surface-strong flex items-start gap-3 rounded-xl border px-3 py-2">
            <div className="ui-surface-muted ui-text-secondary flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-[11px] font-semibold">
              {index + 1}
            </div>
            <div className="min-w-0 flex-1">
              <div className="ui-text-secondary truncate text-sm">{item.key || '-'}</div>
              <div className="ui-text-muted text-xs">
                {valueMode === 'score'
                  ? Number(item.score || 0).toFixed(2)
                  : `x${item.count || 0}`}
              </div>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
};

export default EKGRankingCard;
