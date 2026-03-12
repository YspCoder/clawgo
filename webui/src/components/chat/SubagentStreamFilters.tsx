import React from 'react';
import { Button } from '../ui/Button';

type SubagentStreamFiltersProps = {
  agents: string[];
  allAgentsLabel: string;
  formatAgentName: (agentID: string) => string;
  onReset: () => void;
  onToggle: (agent: string) => void;
  selectedAgents: string[];
};

const SubagentStreamFilters: React.FC<SubagentStreamFiltersProps> = ({
  agents,
  allAgentsLabel,
  formatAgentName,
  onReset,
  onToggle,
  selectedAgents,
}) => {
  return (
    <div className="ui-surface-strong ui-border-subtle px-4 py-3 border-b flex flex-wrap gap-2">
      <Button onClick={onReset} variant={selectedAgents.length === 0 ? 'primary' : 'neutral'} size="xs" radius="full">
        {allAgentsLabel}
      </Button>
      {agents.map((agent) => (
        <Button
          key={agent}
          onClick={() => onToggle(agent)}
          variant={selectedAgents.includes(agent) ? 'primary' : 'neutral'}
          size="xs"
          radius="full"
        >
          {formatAgentName(agent)}
        </Button>
      ))}
    </div>
  );
};

export default SubagentStreamFilters;
