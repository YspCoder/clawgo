import { useEffect, useState } from 'react';
import type { RegistryAgent, RuntimeTask, StreamItem } from './chatUtils';

type UseSubagentChatRuntimeParams = {
  dispatchAgentID: string;
  q: string;
  subagentRegistryItems: unknown[];
  subagentRuntimeItems: unknown[];
  subagentStreamItems: unknown[];
};

export function useSubagentChatRuntime({
  dispatchAgentID,
  q,
  subagentRegistryItems,
  subagentRuntimeItems,
  subagentStreamItems,
}: UseSubagentChatRuntimeParams) {
  const [subagentStream, setSubagentStream] = useState<StreamItem[]>([]);
  const [registryAgents, setRegistryAgents] = useState<RegistryAgent[]>([]);
  const [runtimeTasks, setRuntimeTasks] = useState<RuntimeTask[]>([]);

  const loadSubagentGroup = async () => {
    try {
      if (subagentStreamItems.length > 0) {
        setSubagentStream(Array.isArray(subagentStreamItems) ? (subagentStreamItems as StreamItem[]) : []);
        return;
      }
      const response = await fetch(`/webui/api/subagents_runtime${q}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ action: 'stream_all', limit: 300, task_limit: 36 }),
      });
      if (!response.ok) return;
      const json = await response.json();
      const items = Array.isArray(json?.result?.items) ? json.result.items : [];
      setSubagentStream(items);
    } catch (error) {
      console.error(error);
    }
  };

  const loadRegistryAgents = async () => {
    try {
      if (subagentRegistryItems.length > 0) {
        const filtered = (Array.isArray(subagentRegistryItems) ? subagentRegistryItems : [])
          .filter((item: RegistryAgent) => item?.agent_id && item.enabled !== false) as RegistryAgent[];
        setRegistryAgents(filtered);
        return filtered;
      }
      const response = await fetch(`/webui/api/subagents_runtime${q}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ action: 'registry' }),
      });
      if (!response.ok) return [];
      const json = await response.json();
      const items = Array.isArray(json?.result?.items) ? json.result.items : [];
      const filtered = items.filter((item: RegistryAgent) => item?.agent_id && item.enabled !== false);
      setRegistryAgents(filtered);
      return filtered;
    } catch (error) {
      console.error(error);
      return [];
    }
  };

  const loadRuntimeTasks = async () => {
    try {
      if (subagentRuntimeItems.length > 0) {
        setRuntimeTasks(Array.isArray(subagentRuntimeItems) ? (subagentRuntimeItems as RuntimeTask[]) : []);
        return;
      }
      const response = await fetch(`/webui/api/subagents_runtime${q}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ action: 'list' }),
      });
      if (!response.ok) return;
      const json = await response.json();
      const items = Array.isArray(json?.result?.items) ? json.result.items : [];
      setRuntimeTasks(items);
    } catch (error) {
      console.error(error);
    }
  };

  useEffect(() => {
    if (dispatchAgentID || registryAgents.length === 0) return;
    const first = registryAgents[0]?.agent_id;
    if (first) {
      // noop, caller can mirror this value from registryAgents when needed
    }
  }, [dispatchAgentID, registryAgents]);

  return {
    loadRegistryAgents,
    loadRuntimeTasks,
    loadSubagentGroup,
    registryAgents,
    runtimeTasks,
    subagentStream,
  };
}
