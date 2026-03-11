import React from 'react';
import EmptyState from '../EmptyState';
import InfoBlock from '../InfoBlock';

type AgentTreePanelProps = {
  emptyLabel: string;
  items: any[];
  title: string;
};

const AgentTreePanel: React.FC<AgentTreePanelProps> = ({
  emptyLabel,
  items,
  title,
}) => {
  return (
    <InfoBlock label={title} contentClassName="p-3 space-y-2">
      {items.length > 0 ? items.map((item, index) => (
        <div key={`${item?.agent_id || index}`} className="ui-media-surface rounded-xl border border-zinc-800/80 p-3">
          <div className="text-sm font-medium text-zinc-100">{String(item?.display_name || item?.agent_id || '-')}</div>
          <div className="text-xs text-zinc-500 mt-1">
            {String(item?.agent_id || '-')} · {String(item?.transport || '-')} · {String(item?.role || '-')}
          </div>
        </div>
      )) : (
        <EmptyState message={emptyLabel} className="p-0" />
      )}
    </InfoBlock>
  );
};

export default AgentTreePanel;
