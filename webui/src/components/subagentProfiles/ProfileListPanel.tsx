import React from 'react';
import ListPanel from '../ListPanel';
import PanelHeader from '../PanelHeader';
import SummaryListItem from '../SummaryListItem';
import type { SubagentProfile } from './profileDraft';

type ProfileListPanelProps = {
  emptyLabel: string;
  items: SubagentProfile[];
  onSelect: (profile: SubagentProfile) => void;
  selectedId: string;
  title: string;
};

const ProfileListPanel: React.FC<ProfileListPanelProps> = ({
  emptyLabel,
  items,
  onSelect,
  selectedId,
  title,
}) => {
  return (
    <ListPanel>
      <PanelHeader title={title} />
      <div className="overflow-y-auto max-h-[70vh]">
        {items.map((item) => {
          const active = selectedId === item.agent_id;
          return (
            <SummaryListItem
              key={item.agent_id}
              active={active}
              className="border-b py-2"
              onClick={() => onSelect(item)}
              title={item.agent_id || '-'}
              subtitle={`${item.status || 'active'} · ${item.role || '-'} · ${item.memory_namespace || '-'}`}
            />
          );
        })}
        {items.length === 0 ? (
          <div className="ui-text-muted px-3 py-4 text-sm">{emptyLabel}</div>
        ) : null}
      </div>
    </ListPanel>
  );
};

export default ProfileListPanel;
