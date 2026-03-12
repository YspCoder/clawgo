import React from 'react';
import InsetCard from '../data-display/InsetCard';
import type { StreamItem, SubagentTask } from './subagentTypes';

type StreamPreview = {
  task: SubagentTask | null;
  items: StreamItem[];
  loading?: boolean;
};

type TopologyTooltipProps = {
  agentID?: string;
  formatStreamTime: (ts?: number) => string;
  meta: string[];
  streamPreview?: StreamPreview;
  subtitle: string;
  summarizePreviewText: (value?: string, limit?: number) => string;
  t: (key: string, options?: any) => string;
  title: string;
  transportType?: 'local' | 'remote';
  x: number;
  y: number;
};

const TopologyTooltip: React.FC<TopologyTooltipProps> = ({
  agentID,
  formatStreamTime,
  meta,
  streamPreview,
  subtitle,
  summarizePreviewText,
  t,
  title,
  transportType,
  x,
  y,
}) => {
  const latestItem = streamPreview?.items?.length ? streamPreview.items[streamPreview.items.length - 1] : null;

  return (
    <div
      className="topology-tooltip pointer-events-none fixed z-50 w-[360px] max-w-[min(360px,calc(100vw-24px))] brand-card-subtle border border-zinc-700/80 p-4 backdrop-blur-md transition-opacity duration-200"
      style={{ left: x, top: y }}
    >
      <div className="flex items-center gap-2 mb-2">
        <div className="topology-accent-warning w-2 h-2 rounded-full" />
        <div className="text-sm font-semibold text-zinc-100">{title}</div>
      </div>
      <div className="text-xs text-zinc-400 mb-3 pb-3 border-b border-zinc-800/60">{subtitle}</div>
      <div className="space-y-1.5">
        {meta.map((line, idx) => {
          if (!line.includes('=')) {
            return (
              <div key={idx} className="text-xs text-zinc-300 font-medium">
                {line}
              </div>
            );
          }
          const [key, ...rest] = line.split('=');
          const value = rest.join('=');
          return (
            <div key={idx} className="flex justify-between gap-3 text-xs">
              <span className="text-zinc-500">{key}</span>
              <span className="text-zinc-300 font-medium text-right">{value || '-'}</span>
            </div>
          );
        })}
      </div>
      {transportType === 'local' && agentID ? (
        <div className="mt-4 pt-4 border-t border-zinc-800/60 space-y-3">
          <div className="text-[11px] text-zinc-500 uppercase tracking-wider">{t('internalStream')}</div>
          {streamPreview?.loading ? (
            <div className="text-xs text-zinc-400">Loading internal stream...</div>
          ) : streamPreview?.task ? (
            <>
              <InsetCard className="p-3 space-y-1.5">
                <div className="text-xs text-zinc-300">run={streamPreview.task?.id || '-'}</div>
                <div className="text-xs text-zinc-400">
                  status={streamPreview.task?.status || '-'} · thread={streamPreview.task?.thread_id || '-'}
                </div>
              </InsetCard>
              {latestItem ? (
                <InsetCard className="p-3 space-y-2">
                  <div className="flex items-center justify-between gap-2">
                    <div className="text-xs font-medium text-zinc-200">
                      {latestItem.kind === 'event'
                        ? `${latestItem.event_type || 'event'}${latestItem.status ? ` · ${latestItem.status}` : ''}`
                        : `${latestItem.from_agent || '-'} -> ${latestItem.to_agent || '-'} · ${latestItem.message_type || 'message'}`}
                    </div>
                    <div className="text-[11px] text-zinc-500">
                      {formatStreamTime(latestItem.at)}
                    </div>
                  </div>
                  <div className="text-xs text-zinc-300 leading-5 whitespace-pre-wrap break-words">
                    {summarizePreviewText(
                      latestItem.kind === 'event' ? (latestItem.message || '(no event message)') : (latestItem.content || '(empty message)'),
                      520,
                    )}
                  </div>
                </InsetCard>
              ) : (
                <div className="text-xs text-zinc-400">No internal stream events yet.</div>
              )}
            </>
          ) : (
            <div className="text-xs text-zinc-400">No persisted run for this agent yet.</div>
          )}
        </div>
      ) : null}
    </div>
  );
};

export default TopologyTooltip;
