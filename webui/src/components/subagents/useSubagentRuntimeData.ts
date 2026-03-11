import { useEffect, useMemo, useRef, useState } from 'react';
import type { RegistrySubagent, StreamPreviewState, SubagentTask } from './subagentTypes';
import { buildTaskStats, normalizeTitle, tokenFromQuery } from './topologyUtils';

type UseSubagentRuntimeDataParams = {
  previewAgentID: string;
  q: string;
  subagentRegistryItems: unknown[];
  subagentRuntimeItems: unknown[];
};

export function useSubagentRuntimeData({
  previewAgentID,
  q,
  subagentRegistryItems,
  subagentRuntimeItems,
}: UseSubagentRuntimeDataParams) {
  const [items, setItems] = useState<SubagentTask[]>([]);
  const [registryItems, setRegistryItems] = useState<RegistrySubagent[]>([]);
  const [selectedId, setSelectedId] = useState('');
  const [selectedAgentID, setSelectedAgentID] = useState('');
  const [streamPreviewByAgent, setStreamPreviewByAgent] = useState<Record<string, StreamPreviewState>>({});
  const streamPreviewLoadingRef = useRef<Record<string, string>>({});

  const apiPath = '/webui/api/subagents_runtime';
  const withAction = (action: string) => `${apiPath}${q}${q ? '&' : '?'}action=${encodeURIComponent(action)}`;
  const runtimeItems = Array.isArray(subagentRuntimeItems) ? (subagentRuntimeItems as SubagentTask[]) : [];
  const registrySourceItems = Array.isArray(subagentRegistryItems) ? (subagentRegistryItems as RegistrySubagent[]) : [];

  const refresh = async () => {
    try {
      if (runtimeItems.length > 0 || registrySourceItems.length > 0) {
        const arr = runtimeItems;
        const registry = registrySourceItems;
        setItems(arr);
        setRegistryItems(registry);
        if (registry.length === 0) {
          setSelectedAgentID('');
          setSelectedId('');
        } else {
          const nextAgentID = selectedAgentID && registry.find((item: RegistrySubagent) => item.agent_id === selectedAgentID)
            ? selectedAgentID
            : (registry[0]?.agent_id || '');
          setSelectedAgentID(nextAgentID);
          const nextTask = arr.find((item: SubagentTask) => item.agent_id === nextAgentID);
          setSelectedId(nextTask?.id || '');
        }
        return;
      }

      const [tasksRes, registryRes] = await Promise.all([
        fetch(withAction('list')),
        fetch(withAction('registry')),
      ]);
      if (!tasksRes.ok) throw new Error(await tasksRes.text());
      if (!registryRes.ok) throw new Error(await registryRes.text());

      const tasksJson = await tasksRes.json();
      const registryJson = await registryRes.json();
      const arr = Array.isArray(tasksJson?.result?.items) ? tasksJson.result.items : [];
      const registry = Array.isArray(registryJson?.result?.items) ? registryJson.result.items : [];

      setItems(arr);
      setRegistryItems(registry);
      if (registry.length === 0) {
        setSelectedAgentID('');
        setSelectedId('');
      } else {
        const nextAgentID = selectedAgentID && registry.find((item: RegistrySubagent) => item.agent_id === selectedAgentID)
          ? selectedAgentID
          : (registry[0]?.agent_id || '');
        setSelectedAgentID(nextAgentID);
        const nextTask = arr.find((item: SubagentTask) => item.agent_id === nextAgentID);
        setSelectedId(nextTask?.id || '');
      }
    } catch {
      setItems([
        { id: 'task-1', status: 'running', agent_id: 'worker-1', role: 'worker', task: 'Process data stream', created: Date.now() },
      ]);
    }
  };

  useEffect(() => {
    refresh().catch(() => {});
  }, [q, selectedAgentID, runtimeItems, registrySourceItems]);

  const selected = useMemo(() => items.find((item) => item.id === selectedId) || null, [items, selectedId]);
  const taskStats = useMemo(() => buildTaskStats(items), [items]);
  const recentTaskByAgent = useMemo(() => {
    return items.reduce<Record<string, SubagentTask>>((acc, task) => {
      const agentID = normalizeTitle(task.agent_id, '');
      if (!agentID) return acc;
      const existing = acc[agentID];
      const currentScore = Math.max(task.updated || 0, task.created || 0);
      const existingScore = existing ? Math.max(existing.updated || 0, existing.created || 0) : -1;
      if (!existing || currentScore > existingScore) {
        acc[agentID] = task;
      }
      return acc;
    }, {});
  }, [items]);

  useEffect(() => {
    const selectedTaskID = String(selected?.id || '').trim();
    const previewTask = previewAgentID ? recentTaskByAgent[previewAgentID] || null : null;
    const previewTaskID = String(previewTask?.id || '').trim();

    if (!previewAgentID) {
      return;
    }

    setStreamPreviewByAgent((prev) => ({
      ...prev,
      [previewAgentID]: {
        task: previewTask,
        items: prev[previewAgentID]?.items || [],
        taskID: previewTaskID,
        loading: !!previewTaskID,
      },
    }));

    if (!selectedTaskID && !previewTaskID) return;

    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const url = new URL(`${proto}//${window.location.host}/webui/api/subagents_runtime/live`);
    if (tokenFromQuery(q)) url.searchParams.set('token', tokenFromQuery(q));
    if (selectedTaskID) url.searchParams.set('task_id', selectedTaskID);
    if (previewTaskID) url.searchParams.set('preview_task_id', previewTaskID);

    const ws = new WebSocket(url.toString());
    ws.onmessage = (event) => {
      try {
        const msg = JSON.parse(event.data);
        const payload = msg?.payload || {};
        if (previewAgentID && payload.preview) {
          setStreamPreviewByAgent((prev) => ({
            ...prev,
            [previewAgentID]: {
              task: payload.preview.task || previewTask,
              items: Array.isArray(payload.preview.items) ? payload.preview.items : [],
              taskID: previewTaskID,
              loading: false,
            },
          }));
        }
      } catch (err) {
        console.error(err);
      }
    };
    ws.onerror = () => {
      if (previewAgentID) {
        setStreamPreviewByAgent((prev) => ({
          ...prev,
          [previewAgentID]: {
            task: previewTask,
            items: prev[previewAgentID]?.items || [],
            taskID: previewTaskID,
            loading: false,
          },
        }));
      }
    };
    return () => {
      ws.close();
    };
  }, [previewAgentID, q, recentTaskByAgent, selected?.id]);

  return {
    items,
    recentTaskByAgent,
    refresh,
    registryItems,
    selected,
    selectedAgentID,
    selectedId,
    setSelectedAgentID,
    setSelectedId,
    streamPreviewByAgent,
    taskStats,
  };
}
