import React from 'react';
import { Button } from '../ui/Button';
import { SelectField, TextField, TextareaField } from '../ui/FormControls';
import type { AgentRuntimeBadge, RegistryAgent } from './chatUtils';

type SubagentSidebarProps = {
  agentLabel: string;
  agentsLabel: string;
  dispatchAgentID: string;
  dispatchHint: string;
  dispatchLabel: string;
  dispatchTask: string;
  dispatchTitle: string;
  dispatchToSubagentLabel: string;
  formatAgentName: (value?: string) => string;
  idleLabel: string;
  onAgentChange: (value: string) => void;
  onDispatch: () => void;
  onLabelChange: (value: string) => void;
  onTaskChange: (value: string) => void;
  registryAgents: RegistryAgent[];
  runtimeBadgeByAgent: Record<string, AgentRuntimeBadge>;
  subagentLabelPlaceholder: string;
  subagentTaskPlaceholder: string;
};

function badgeClassName(status?: AgentRuntimeBadge['status']) {
  switch (status) {
    case 'running':
      return 'ui-pill-success';
    case 'waiting':
      return 'ui-pill-warning';
    case 'failed':
      return 'ui-pill-danger';
    case 'completed':
      return 'ui-pill-info';
    default:
      return 'ui-pill-neutral';
  }
}

const SubagentSidebar: React.FC<SubagentSidebarProps> = ({
  agentLabel,
  agentsLabel,
  dispatchAgentID,
  dispatchHint,
  dispatchLabel,
  dispatchTask,
  dispatchTitle,
  dispatchToSubagentLabel,
  formatAgentName,
  idleLabel,
  onAgentChange,
  onDispatch,
  onLabelChange,
  onTaskChange,
  registryAgents,
  runtimeBadgeByAgent,
  subagentLabelPlaceholder,
  subagentTaskPlaceholder,
}) => {
  return (
    <div className="bg-zinc-50/50 dark:bg-zinc-900/40 ui-border-subtle w-full xl:w-[320px] xl:shrink-0 border-b xl:border-b-0 xl:border-r p-4 flex flex-col gap-4 max-h-[46vh] xl:max-h-none overflow-y-auto">
      <div>
        <div className="ui-text-muted text-xs uppercase tracking-wider mb-1">{dispatchTitle}</div>
        <div className="ui-text-secondary text-sm">{dispatchHint}</div>
      </div>
      <div className="space-y-3">
        <SelectField
          value={dispatchAgentID}
          onChange={(e) => onAgentChange(e.target.value)}
          className="w-full rounded-2xl py-2.5"
        >
          {registryAgents.map((agent) => (
            <option key={agent.agent_id} value={agent.agent_id}>
              {formatAgentName(agent.display_name || agent.agent_id)} · {agent.role || '-'}
            </option>
          ))}
        </SelectField>
        <TextareaField
          value={dispatchTask}
          onChange={(e) => onTaskChange(e.target.value)}
          placeholder={subagentTaskPlaceholder}
          className="w-full min-h-[180px] resize-none rounded-2xl px-3 py-3"
        />
        <TextField
          value={dispatchLabel}
          onChange={(e) => onLabelChange(e.target.value)}
          placeholder={subagentLabelPlaceholder}
          className="w-full rounded-2xl py-2.5"
        />
        <Button onClick={onDispatch} disabled={!dispatchAgentID.trim() || !dispatchTask.trim()} variant="primary" size="md_tall" fullWidth>
          {dispatchToSubagentLabel}
        </Button>
      </div>
      <div className="ui-border-subtle border-t pt-4 min-h-0 flex flex-col">
        <div className="ui-text-muted text-xs uppercase tracking-wider mb-2">{agentsLabel}</div>
        <div className="overflow-y-auto space-y-2 min-h-0">
          {registryAgents.map((agent) => {
            const active = dispatchAgentID === agent.agent_id;
            const badge = runtimeBadgeByAgent[String(agent.agent_id || '')];
            return (
              <button
                key={agent.agent_id}
                onClick={() => onAgentChange(String(agent.agent_id || ''))}
                className={`w-full text-left rounded-2xl border px-3 py-2.5 transition-all duration-300 hover-lift ${active ? 'ui-card-active-warning shadow-sm border-amber-500/30' : 'ui-border-subtle bg-zinc-900/40 hover:bg-zinc-800/60 hover:border-zinc-700'}`}
              >
                <div className="flex items-center justify-between gap-2 mb-0.5">
                  <div className="ui-text-primary text-sm font-semibold truncate">{formatAgentName(agent.display_name || agent.agent_id)}</div>
                  <span className={`ui-pill shrink-0 inline-flex items-center rounded-full border px-2 py-0.5 text-[10px] uppercase font-bold tracking-wider ${badgeClassName(badge?.status)}`}>
                    {badge?.text || idleLabel}
                  </span>
                </div>
                <div className="ui-text-muted text-xs truncate">{agent.agent_id} · {agent.role || '-'}</div>
              </button>
            );
          })}
        </div>
      </div>
    </div>
  );
};

export default SubagentSidebar;
