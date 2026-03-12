import React from 'react';
import EmptyState from '../data-display/EmptyState';
import InfoBlock from '../data-display/InfoBlock';

type NodeP2PPanelProps = {
  emptyLabel: string;
  errorLabel: string;
  readyLabel: string;
  retriesLabel: string;
  selectedSession: any;
  statusLabel: string;
  timeFormatter: (value: unknown) => string;
  title: string;
};

const NodeP2PPanel: React.FC<NodeP2PPanelProps> = ({
  emptyLabel,
  errorLabel,
  readyLabel,
  retriesLabel,
  selectedSession,
  statusLabel,
  timeFormatter,
  title,
}) => {
  return (
    <InfoBlock label={title} contentClassName="p-3">
      {selectedSession ? (
        <div className="grid grid-cols-2 gap-3 text-xs">
          <div>
            <div className="text-zinc-500">{statusLabel}</div>
            <div>{String(selectedSession.status || 'unknown')}</div>
          </div>
          <div>
            <div className="text-zinc-500">{retriesLabel}</div>
            <div>{Number(selectedSession.retry_count || 0)}</div>
          </div>
          <div>
            <div className="text-zinc-500">{readyLabel}</div>
            <div>{timeFormatter(selectedSession.last_ready_at)}</div>
          </div>
          <div>
            <div className="text-zinc-500">{errorLabel}</div>
            <div className="break-all">{String(selectedSession.last_error || '-')}</div>
          </div>
        </div>
      ) : (
        <EmptyState message={emptyLabel} className="p-0" />
      )}
    </InfoBlock>
  );
};

export default NodeP2PPanel;
