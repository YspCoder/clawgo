import React, { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import { useUI } from '../context/UIContext';

type SubagentTask = {
  id: string;
  status?: string;
  label?: string;
  role?: string;
  agent_id?: string;
  session_key?: string;
  memory_ns?: string;
  tool_allowlist?: string[];
  max_retries?: number;
  retry_count?: number;
  retry_backoff?: number;
  timeout_sec?: number;
  max_task_chars?: number;
  max_result_chars?: number;
  created?: number;
  updated?: number;
  task?: string;
  result?: string;
  thread_id?: string;
  correlation_id?: string;
  waiting_for_reply?: boolean;
};

type RouterReply = {
  task_id?: string;
  thread_id?: string;
  correlation_id?: string;
  agent_id?: string;
  status?: string;
  result?: string;
};

type AgentThread = {
  thread_id?: string;
  owner?: string;
  participants?: string[];
  status?: string;
  topic?: string;
};

type AgentMessage = {
  message_id?: string;
  thread_id?: string;
  from_agent?: string;
  to_agent?: string;
  reply_to?: string;
  correlation_id?: string;
  type?: string;
  content?: string;
  requires_reply?: boolean;
  status?: string;
  created_at?: number;
};

type RegistrySubagent = {
  agent_id?: string;
  enabled?: boolean;
  type?: string;
  transport?: string;
  node_id?: string;
  parent_agent_id?: string;
  managed_by?: string;
  display_name?: string;
  role?: string;
  description?: string;
  system_prompt?: string;
  system_prompt_file?: string;
  prompt_file_found?: boolean;
  memory_namespace?: string;
  tool_allowlist?: string[];
  routing_keywords?: string[];
};

type AgentTreeNode = {
  agent_id?: string;
  display_name?: string;
  role?: string;
  type?: string;
  transport?: string;
  managed_by?: string;
  node_id?: string;
  enabled?: boolean;
  children?: AgentTreeNode[];
};

type NodeTree = {
  node_id?: string;
  node_name?: string;
  online?: boolean;
  source?: string;
  readonly?: boolean;
  root?: {
    root?: AgentTreeNode;
  };
};

type NodeInfo = {
  id?: string;
  name?: string;
  endpoint?: string;
  version?: string;
  online?: boolean;
};

type AgentTaskStats = {
  total: number;
  running: number;
  failed: number;
  waiting: number;
  active: Array<{ id: string; status: string; title: string }>;
};

type GraphCardSpec = {
  key: string;
  branch: string;
  agentID?: string;
  transportType?: 'local' | 'remote';
  x: number;
  y: number;
  w: number;
  h: number;
  kind: 'node' | 'agent';
  title: string;
  subtitle: string;
  meta: string[];
  accent: string;
  online?: boolean;
  clickable?: boolean;
  highlighted?: boolean;
  dimmed?: boolean;
  hidden?: boolean;
  onClick?: () => void;
};

type GraphLineSpec = {
  x1: number;
  y1: number;
  x2: number;
  y2: number;
  dashed?: boolean;
  branch: string;
  highlighted?: boolean;
  dimmed?: boolean;
  hidden?: boolean;
};

const cardWidth = 230;
const cardHeight = 112;
const clusterWidth = 350;
const topY = 24;
const mainY = 172;
const childStartY = 334;
const childGap = 132;

function normalizeTitle(value?: string, fallback = '-'): string {
  const trimmed = `${value || ''}`.trim();
  return trimmed || fallback;
}

function summarizeTask(task?: string, label?: string): string {
  const text = normalizeTitle(label || task, '-');
  return text.length > 52 ? `${text.slice(0, 49)}...` : text;
}

function buildTaskStats(tasks: SubagentTask[]): Record<string, AgentTaskStats> {
  return tasks.reduce<Record<string, AgentTaskStats>>((acc, task) => {
    const agentID = normalizeTitle(task.agent_id, '');
    if (!agentID) return acc;
    if (!acc[agentID]) {
      acc[agentID] = { total: 0, running: 0, failed: 0, waiting: 0, active: [] };
    }
    const item = acc[agentID];
    item.total += 1;
    if (task.status === 'running') item.running += 1;
    if (task.status === 'failed') item.failed += 1;
    if (task.waiting_for_reply) item.waiting += 1;
    if (task.status === 'running' || task.waiting_for_reply) {
      item.active.push({
        id: task.id,
        status: task.status || '-',
        title: summarizeTask(task.task, task.label),
      });
    }
    return acc;
  }, {});
}

function GraphCard({ card }: { card: GraphCardSpec }) {
  return (
    <foreignObject x={card.x} y={card.y} width={card.w} height={card.h}>
      <button
        type="button"
        onClick={card.onClick}
        disabled={!card.clickable}
        className={`h-full w-full rounded-2xl border text-left p-3 shadow-sm ${
          card.clickable ? 'cursor-pointer hover:border-zinc-600' : 'cursor-default'
        }`}
        style={{
          background: card.highlighted ? 'rgba(39,39,42,0.98)' : 'rgba(24,24,27,0.92)',
          borderColor: card.highlighted ? 'rgba(251,191,36,0.85)' : 'rgba(63,63,70,0.9)',
          boxShadow: card.highlighted
            ? '0 0 0 1px rgba(251,191,36,0.35), 0 12px 30px rgba(0,0,0,0.28)'
            : card.online
              ? '0 0 0 1px rgba(16,185,129,0.15)'
              : undefined,
          opacity: card.dimmed ? 0.38 : 1,
          transform: card.highlighted ? 'translateY(-2px)' : undefined,
          transition: 'all 160ms ease',
        }}
      >
        <div className="flex items-center justify-between gap-2">
          <div className="min-w-0">
            <div className="truncate text-sm font-semibold text-zinc-100">{card.title}</div>
            <div className="truncate text-[11px] text-zinc-500">{card.subtitle}</div>
          </div>
          <div className={`h-2.5 w-2.5 rounded-full ${card.accent}`} />
        </div>
        <div className="mt-3 space-y-1">
          {card.meta.slice(0, 4).map((line, idx) => (
            <div key={`${card.key}-${idx}`} className="truncate text-[11px] text-zinc-300">
              {line}
            </div>
          ))}
        </div>
      </button>
    </foreignObject>
  );
}

const Subagents: React.FC = () => {
  const { t } = useTranslation();
  const { q, nodeTrees, nodes } = useAppContext();
  const ui = useUI();

  const [items, setItems] = useState<SubagentTask[]>([]);
  const [selectedId, setSelectedId] = useState<string>('');
  const [spawnTask, setSpawnTask] = useState('');
  const [spawnAgentID, setSpawnAgentID] = useState('');
  const [spawnRole, setSpawnRole] = useState('');
  const [spawnLabel, setSpawnLabel] = useState('');
  const [steerMessage, setSteerMessage] = useState('');
  const [dispatchTask, setDispatchTask] = useState('');
  const [dispatchAgentID, setDispatchAgentID] = useState('');
  const [dispatchRole, setDispatchRole] = useState('');
  const [dispatchWaitTimeout, setDispatchWaitTimeout] = useState('120');
  const [dispatchReply, setDispatchReply] = useState<RouterReply | null>(null);
  const [dispatchMerged, setDispatchMerged] = useState('');
  const [threadDetail, setThreadDetail] = useState<AgentThread | null>(null);
  const [threadMessages, setThreadMessages] = useState<AgentMessage[]>([]);
  const [inboxMessages, setInboxMessages] = useState<AgentMessage[]>([]);
  const [replyMessage, setReplyMessage] = useState('');
  const [replyToMessageID, setReplyToMessageID] = useState('');
  const [configAgentID, setConfigAgentID] = useState('');
  const [configRole, setConfigRole] = useState('');
  const [configDisplayName, setConfigDisplayName] = useState('');
  const [configSystemPrompt, setConfigSystemPrompt] = useState('');
  const [configSystemPromptFile, setConfigSystemPromptFile] = useState('');
  const [configToolAllowlist, setConfigToolAllowlist] = useState('');
  const [configRoutingKeywords, setConfigRoutingKeywords] = useState('');
  const [registryItems, setRegistryItems] = useState<RegistrySubagent[]>([]);
  const [promptFileContent, setPromptFileContent] = useState('');
  const [promptFileFound, setPromptFileFound] = useState(false);
  const [selectedTopologyBranch, setSelectedTopologyBranch] = useState('');
  const [topologyFilter, setTopologyFilter] = useState<'all' | 'running' | 'failed' | 'local' | 'remote'>('all');

  const apiPath = '/webui/api/subagents_runtime';
  const withAction = (action: string) => `${apiPath}${q}${q ? '&' : '?'}action=${encodeURIComponent(action)}`;

  const load = async () => {
    const [tasksRes, registryRes] = await Promise.all([
      fetch(withAction('list')),
      fetch(withAction('registry')),
    ]);
    if (!tasksRes.ok) throw new Error(await tasksRes.text());
    if (!registryRes.ok) throw new Error(await registryRes.text());
    const j = await tasksRes.json();
    const registryJson = await registryRes.json();
    const arr = Array.isArray(j?.result?.items) ? j.result.items : [];
    const registryItems = Array.isArray(registryJson?.result?.items) ? registryJson.result.items : [];
    setItems(arr);
    setRegistryItems(registryItems);
    if (arr.length === 0) {
      setSelectedId('');
    } else if (!arr.find((x: SubagentTask) => x.id === selectedId)) {
      setSelectedId(arr[0].id || '');
    }
  };

  useEffect(() => {
    load().catch(() => {});
  }, [q]);

  useEffect(() => {
    const interval = window.setInterval(() => {
      load().catch(() => {});
    }, 5000);
    return () => window.clearInterval(interval);
  }, [q]);

  const selected = useMemo(() => items.find((x) => x.id === selectedId) || null, [items, selectedId]);
  const parsedNodeTrees = useMemo<NodeTree[]>(() => {
    try {
      const parsed = JSON.parse(nodeTrees);
      return Array.isArray(parsed) ? parsed : [];
    } catch {
      return [];
    }
  }, [nodeTrees]);
  const parsedNodes = useMemo<NodeInfo[]>(() => {
    try {
      const parsed = JSON.parse(nodes);
      return Array.isArray(parsed) ? parsed : [];
    } catch {
      return [];
    }
  }, [nodes]);
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
  const topologyGraph = useMemo(() => {
    const localTree = parsedNodeTrees.find((tree) => normalizeTitle(tree.node_id, '') === 'local') || null;
    const remoteTrees = parsedNodeTrees.filter((tree) => normalizeTitle(tree.node_id, '') !== 'local');
    const localRoot = localTree?.root?.root || {
      agent_id: 'main',
      display_name: 'Main Agent',
      role: 'orchestrator',
      type: 'router',
      transport: 'local',
      enabled: true,
      children: registryItems
        .filter((item) => item.agent_id && item.agent_id !== 'main' && item.managed_by === 'config.json')
        .map((item) => ({
          agent_id: item.agent_id,
          display_name: item.display_name,
          role: item.role,
          type: item.type,
          transport: item.transport,
          enabled: item.enabled,
          children: [],
        })),
    };
    const localNode = parsedNodes.find((node) => normalizeTitle(node.id, '') === 'local') || {
      id: 'local',
      name: 'local',
      online: true,
    };
    const clusterCount = Math.max(1, remoteTrees.length + 1);
    const localChildren = Array.isArray(localRoot.children) ? localRoot.children : [];
    const remoteChildMax = remoteTrees.reduce((max, tree) => {
      const count = Array.isArray(tree.root?.root?.children) ? tree.root?.root?.children?.length || 0 : 0;
      return Math.max(max, count);
    }, 0);
    const maxChildren = Math.max(localChildren.length, remoteChildMax, 1);
    const width = clusterCount * clusterWidth + 120;
    const height = childStartY + maxChildren * childGap + 30;
    const mainClusterX = 56;
    const localNodeX = mainClusterX + 20;
    const localMainX = mainClusterX + 20;
    const localMainCenterX = localMainX + cardWidth / 2;
    const localMainCenterY = mainY + cardHeight / 2;
    const cards: GraphCardSpec[] = [];
    const lines: GraphLineSpec[] = [];
    const localBranch = 'local';
    const localBranchStats = {
      running: 0,
      failed: 0,
    };

    const localNodeCard: GraphCardSpec = {
      key: 'node-local',
      branch: localBranch,
      transportType: 'local',
      kind: 'node',
      x: localNodeX,
      y: topY,
      w: cardWidth,
      h: cardHeight,
      title: normalizeTitle(localNode.name, 'local'),
      subtitle: normalizeTitle(localNode.id, 'local'),
      meta: [
        localNode.online ? t('online') : t('offline'),
        localNode.endpoint ? `endpoint=${localNode.endpoint}` : 'endpoint=-',
        localNode.version ? `version=${localNode.version}` : 'version=-',
        `${t('childrenCount')}=${localChildren.length}`,
      ],
      accent: localNode.online ? 'bg-emerald-500' : 'bg-red-500',
      online: !!localNode.online,
      clickable: true,
      onClick: () => setSelectedTopologyBranch(localBranch),
    };
    const localMainStats = taskStats[normalizeTitle(localRoot.agent_id, 'main')] || { total: 0, running: 0, failed: 0, waiting: 0, active: [] };
    const localMainTask = recentTaskByAgent[normalizeTitle(localRoot.agent_id, 'main')];
    localBranchStats.running += localMainStats.running;
    localBranchStats.failed += localMainStats.failed;
    const localMainCard: GraphCardSpec = {
      key: 'agent-main',
      branch: localBranch,
      agentID: normalizeTitle(localRoot.agent_id, 'main'),
      transportType: 'local',
      kind: 'agent',
      x: localMainX,
      y: mainY,
      w: cardWidth,
      h: cardHeight,
      title: normalizeTitle(localRoot.display_name, 'Main Agent'),
      subtitle: `${normalizeTitle(localRoot.agent_id, 'main')} · ${normalizeTitle(localRoot.role, '-')}`,
      meta: [
        `total=${localMainStats.total} running=${localMainStats.running}`,
        `waiting=${localMainStats.waiting} failed=${localMainStats.failed}`,
        `transport=${normalizeTitle(localRoot.transport, 'local')} type=${normalizeTitle(localRoot.type, 'router')}`,
        localMainStats.active[0] ? `task: ${localMainStats.active[0].title}` : t('noLiveTasks'),
      ],
      accent: 'bg-amber-400',
      clickable: true,
      onClick: () => {
        setSelectedTopologyBranch(localBranch);
        if (localMainTask?.id) setSelectedId(localMainTask.id);
      },
    };
    cards.push(localNodeCard, localMainCard);
    lines.push({
      x1: localNodeCard.x + cardWidth / 2,
      y1: localNodeCard.y + cardHeight,
      x2: localMainCard.x + cardWidth / 2,
      y2: localMainCard.y,
      branch: localBranch,
    });

    localChildren.forEach((child, idx) => {
      const childX = mainClusterX + 20;
      const childY = childStartY + idx * childGap;
      const stats = taskStats[normalizeTitle(child.agent_id, '')] || { total: 0, running: 0, failed: 0, waiting: 0, active: [] };
      const task = recentTaskByAgent[normalizeTitle(child.agent_id, '')];
      localBranchStats.running += stats.running;
      localBranchStats.failed += stats.failed;
      cards.push({
        key: `local-child-${child.agent_id || idx}`,
        branch: localBranch,
        agentID: normalizeTitle(child.agent_id, ''),
        transportType: 'local',
        kind: 'agent',
        x: childX,
        y: childY,
        w: cardWidth,
        h: cardHeight,
        title: normalizeTitle(child.display_name, normalizeTitle(child.agent_id, 'agent')),
        subtitle: `${normalizeTitle(child.agent_id, '-')} · ${normalizeTitle(child.role, '-')}`,
        meta: [
          `total=${stats.total} running=${stats.running}`,
          `waiting=${stats.waiting} failed=${stats.failed}`,
          `transport=${normalizeTitle(child.transport, 'local')} type=${normalizeTitle(child.type, 'worker')}`,
          stats.active[0] ? `task: ${stats.active[0].title}` : task ? `last: ${summarizeTask(task.task, task.label)}` : t('noLiveTasks'),
        ],
        accent: stats.running > 0 ? 'bg-emerald-500' : stats.failed > 0 ? 'bg-red-500' : 'bg-sky-400',
        clickable: true,
        onClick: () => {
          setSelectedTopologyBranch(localBranch);
          if (task?.id) setSelectedId(task.id);
        },
      });
      lines.push({
        x1: localMainCenterX,
        y1: localMainCenterY,
        x2: childX + cardWidth / 2,
        y2: childY,
        branch: localBranch,
      });
    });

    remoteTrees.forEach((tree, treeIndex) => {
      const branch = `node:${normalizeTitle(tree.node_id, `remote-${treeIndex}`)}`;
      const baseX = 56 + (treeIndex + 1) * clusterWidth;
      const nodeX = baseX + 20;
      const rootX = baseX + 20;
      const treeRoot = tree.root?.root;
      const remoteNodeCard: GraphCardSpec = {
        key: `node-${tree.node_id || treeIndex}`,
        branch,
        transportType: 'remote',
        kind: 'node',
        x: nodeX,
        y: topY,
        w: cardWidth,
        h: cardHeight,
        title: normalizeTitle(tree.node_name, tree.node_id || 'node'),
        subtitle: normalizeTitle(tree.node_id, 'node'),
        meta: [
          tree.online ? t('online') : t('offline'),
          normalizeTitle(tree.source, '-'),
          tree.readonly ? t('readonlyMirror') : t('localControl'),
          `${t('childrenCount')}=${Array.isArray(treeRoot?.children) ? treeRoot?.children?.length || 0 : 0}`,
        ],
        accent: tree.online ? 'bg-emerald-500' : 'bg-red-500',
        online: !!tree.online,
        clickable: true,
        onClick: () => setSelectedTopologyBranch(branch),
      };
      cards.push(remoteNodeCard);
      lines.push({
        x1: localMainCenterX,
        y1: localMainCenterY,
        x2: remoteNodeCard.x,
        y2: remoteNodeCard.y + cardHeight / 2,
        dashed: true,
        branch,
      });
      if (!treeRoot) return;
      const rootCard: GraphCardSpec = {
        key: `remote-root-${tree.node_id || treeIndex}`,
        branch,
        agentID: normalizeTitle(treeRoot.agent_id, ''),
        transportType: 'remote',
        kind: 'agent',
        x: rootX,
        y: mainY,
        w: cardWidth,
        h: cardHeight,
        title: normalizeTitle(treeRoot.display_name, treeRoot.agent_id || 'main'),
        subtitle: `${normalizeTitle(treeRoot.agent_id, '-')} · ${normalizeTitle(treeRoot.role, '-')}`,
        meta: [
          `transport=${normalizeTitle(treeRoot.transport, 'node')} type=${normalizeTitle(treeRoot.type, 'router')}`,
          `source=${normalizeTitle(treeRoot.managed_by, tree.source || '-')}`,
          t('remoteTasksUnavailable'),
        ],
        accent: tree.online ? 'bg-fuchsia-400' : 'bg-zinc-500',
        clickable: true,
        onClick: () => setSelectedTopologyBranch(branch),
      };
      cards.push(rootCard);
      lines.push({
        x1: remoteNodeCard.x + cardWidth / 2,
        y1: remoteNodeCard.y + cardHeight,
        x2: rootCard.x + cardWidth / 2,
        y2: rootCard.y,
        branch,
      });
      const children = Array.isArray(treeRoot.children) ? treeRoot.children : [];
      children.forEach((child, idx) => {
        const childX = baseX + 20;
        const childY = childStartY + idx * childGap;
        cards.push({
          key: `remote-child-${tree.node_id || treeIndex}-${child.agent_id || idx}`,
          branch,
          agentID: normalizeTitle(child.agent_id, ''),
          transportType: 'remote',
          kind: 'agent',
          x: childX,
          y: childY,
          w: cardWidth,
          h: cardHeight,
          title: normalizeTitle(child.display_name, child.agent_id || 'agent'),
          subtitle: `${normalizeTitle(child.agent_id, '-')} · ${normalizeTitle(child.role, '-')}`,
          meta: [
            `transport=${normalizeTitle(child.transport, 'node')} type=${normalizeTitle(child.type, 'worker')}`,
            `source=${normalizeTitle(child.managed_by, 'remote_webui')}`,
            t('remoteTasksUnavailable'),
          ],
          accent: 'bg-violet-400',
          clickable: true,
          onClick: () => setSelectedTopologyBranch(branch),
        });
        lines.push({
          x1: rootCard.x + cardWidth / 2,
          y1: rootCard.y + cardHeight / 2,
          x2: childX + cardWidth / 2,
          y2: childY,
          branch,
        });
      });
    });

    const highlightedBranch = selectedTopologyBranch.trim();
    const branchFilters = new Map<string, boolean>();
    branchFilters.set(localBranch, topologyFilter === 'all' || topologyFilter === 'local' || (topologyFilter === 'running' && localBranchStats.running > 0) || (topologyFilter === 'failed' && localBranchStats.failed > 0));
    remoteTrees.forEach((tree, treeIndex) => {
      const branch = `node:${normalizeTitle(tree.node_id, `remote-${treeIndex}`)}`;
      branchFilters.set(branch, topologyFilter === 'all' || topologyFilter === 'remote');
    });
    const decoratedCards = cards.map((card) => ({
      ...card,
      hidden: branchFilters.get(card.branch) === false,
      highlighted: !highlightedBranch || card.branch === highlightedBranch,
      dimmed: branchFilters.get(card.branch) === false ? true : !!highlightedBranch && card.branch !== highlightedBranch,
    }));
    const decoratedLines = lines.map((line) => ({
      ...line,
      hidden: branchFilters.get(line.branch) === false,
      highlighted: !highlightedBranch || line.branch === highlightedBranch,
      dimmed: branchFilters.get(line.branch) === false ? true : !!highlightedBranch && line.branch !== highlightedBranch,
    }));

    return { width, height, cards: decoratedCards, lines: decoratedLines };
  }, [parsedNodeTrees, parsedNodes, registryItems, taskStats, recentTaskByAgent, selectedTopologyBranch, topologyFilter, t]);

  const callAction = async (payload: Record<string, any>) => {
    const r = await fetch(`${apiPath}${q}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
    if (!r.ok) {
      await ui.notify({ title: t('requestFailed'), message: await r.text() });
      return null;
    }
    return r.json();
  };

  const loadThreadAndInbox = async (task: SubagentTask | null) => {
    if (!task?.id) {
      setThreadDetail(null);
      setThreadMessages([]);
      setInboxMessages([]);
      return;
    }
    const [threadRes, inboxRes] = await Promise.all([
      callAction({ action: 'thread', id: task.id, limit: 50 }),
      callAction({ action: 'inbox', id: task.id, limit: 50 }),
    ]);
    setThreadDetail(threadRes?.result?.thread || null);
    setThreadMessages(Array.isArray(threadRes?.result?.messages) ? threadRes.result.messages : []);
    setInboxMessages(Array.isArray(inboxRes?.result?.messages) ? inboxRes.result.messages : []);
  };

  useEffect(() => {
    loadThreadAndInbox(selected).catch(() => {});
  }, [selectedId, q, items]);

  useEffect(() => {
    const path = configSystemPromptFile.trim();
    if (!path) {
      setPromptFileContent('');
      setPromptFileFound(false);
      return;
    }
    callAction({ action: 'prompt_file_get', path })
      .then((data) => {
        const found = data?.result?.found === true;
        setPromptFileFound(found);
        setPromptFileContent(found ? data?.result?.content || '' : '');
      })
      .catch(() => {});
  }, [configSystemPromptFile, q]);

  const spawn = async () => {
    if (!spawnTask.trim()) {
      await ui.notify({ title: t('requestFailed'), message: 'task is required' });
      return;
    }
    const data = await callAction({
      action: 'spawn',
      task: spawnTask,
      agent_id: spawnAgentID,
      role: spawnRole,
      label: spawnLabel,
    });
    if (!data) return;
    setSpawnTask('');
    await load();
  };

  const kill = async () => {
    if (!selected?.id) return;
    await callAction({ action: 'kill', id: selected.id });
    await load();
  };

  const resume = async () => {
    if (!selected?.id) return;
    await callAction({ action: 'resume', id: selected.id });
    await load();
  };

  const steer = async () => {
    if (!selected?.id || !steerMessage.trim()) return;
    await callAction({ action: 'steer', id: selected.id, message: steerMessage });
    setSteerMessage('');
    await load();
    await loadThreadAndInbox(selected);
  };

  const dispatchAndWait = async () => {
    if (!dispatchTask.trim()) {
      await ui.notify({ title: t('requestFailed'), message: 'task is required' });
      return;
    }
    const waitTimeout = Number.parseInt(dispatchWaitTimeout, 10);
    const data = await callAction({
      action: 'dispatch_and_wait',
      task: dispatchTask,
      agent_id: dispatchAgentID,
      role: dispatchRole,
      wait_timeout_sec: Number.isFinite(waitTimeout) && waitTimeout > 0 ? waitTimeout : 120,
    });
    if (!data) return;
    setDispatchReply(data?.result?.reply || null);
    setDispatchMerged(data?.result?.merged || '');
    await load();
  };

  const sendMessage = async () => {
    if (!selected?.id || !replyMessage.trim()) return;
    await callAction({ action: 'send', id: selected.id, message: replyMessage });
    setReplyMessage('');
    setReplyToMessageID('');
    await load();
    await loadThreadAndInbox(selected);
  };

  const replyToMessage = async () => {
    if (!selected?.id || !replyMessage.trim()) return;
    await callAction({ action: 'reply', id: selected.id, message_id: replyToMessageID, message: replyMessage });
    setReplyMessage('');
    setReplyToMessageID('');
    await load();
    await loadThreadAndInbox(selected);
  };

  const ackMessage = async (messageID: string) => {
    if (!selected?.id || !messageID) return;
    await callAction({ action: 'ack', id: selected.id, message_id: messageID });
    await load();
    await loadThreadAndInbox(selected);
  };

  const upsertConfigSubagent = async () => {
    if (!configAgentID.trim()) {
      await ui.notify({ title: t('requestFailed'), message: 'agent_id is required' });
      return;
    }
    const toolAllowlist = configToolAllowlist
      .split(',')
      .map((item) => item.trim())
      .filter(Boolean);
    const routingKeywords = configRoutingKeywords
      .split(',')
      .map((item) => item.trim())
      .filter(Boolean);
    const data = await callAction({
      action: 'upsert_config_subagent',
      agent_id: configAgentID,
      role: configRole,
      display_name: configDisplayName,
      system_prompt: configSystemPrompt,
      system_prompt_file: configSystemPromptFile,
      tool_allowlist: toolAllowlist,
      routing_keywords: routingKeywords,
    });
    if (!data) return;
    await ui.notify({ title: t('saved'), message: t('configSubagentSaved') });
    setConfigAgentID('');
    setConfigRole('');
    setConfigDisplayName('');
    setConfigSystemPrompt('');
    setConfigSystemPromptFile('');
    setConfigToolAllowlist('');
    setConfigRoutingKeywords('');
    await load();
  };

  const loadRegistryItem = (item: RegistrySubagent) => {
    setConfigAgentID(item.agent_id || '');
    setConfigRole(item.role || '');
    setConfigDisplayName(item.display_name || '');
    setConfigSystemPrompt(item.system_prompt || '');
    setConfigSystemPromptFile(item.system_prompt_file || '');
    setConfigToolAllowlist(Array.isArray(item.tool_allowlist) ? item.tool_allowlist.join(', ') : '');
    setConfigRoutingKeywords(Array.isArray(item.routing_keywords) ? item.routing_keywords.join(', ') : '');
  };

  const setRegistryEnabled = async (item: RegistrySubagent, enabled: boolean) => {
    if (!item.agent_id) return;
    const data = await callAction({ action: 'set_config_subagent_enabled', agent_id: item.agent_id, enabled });
    if (!data) return;
    await load();
  };

  const deleteRegistryItem = async (item: RegistrySubagent) => {
    if (!item.agent_id) return;
    const ok = await ui.confirmDialog({
      title: t('deleteAgent'),
      message: t('deleteAgentConfirm', { id: item.agent_id }),
      danger: true,
      confirmText: t('delete'),
    });
    if (!ok) return;
    const data = await callAction({ action: 'delete_config_subagent', agent_id: item.agent_id });
    if (!data) return;
    await load();
  };

  const savePromptFile = async () => {
    if (!configSystemPromptFile.trim()) {
      await ui.notify({ title: t('requestFailed'), message: 'system_prompt_file is required' });
      return;
    }
    const data = await callAction({
      action: 'prompt_file_set',
      path: configSystemPromptFile,
      content: promptFileContent,
    });
    if (!data) return;
    setPromptFileFound(true);
    await ui.notify({ title: t('saved'), message: t('promptFileSaved') });
    await load();
  };

  const bootstrapPromptFile = async () => {
    if (!configAgentID.trim()) {
      await ui.notify({ title: t('requestFailed'), message: 'agent_id is required' });
      return;
    }
    const data = await callAction({
      action: 'prompt_file_bootstrap',
      agent_id: configAgentID,
      role: configRole,
      display_name: configDisplayName,
      path: configSystemPromptFile,
    });
    if (!data) return;
    const path = data?.result?.path || configSystemPromptFile || `agents/${configAgentID}/AGENT.md`;
    setConfigSystemPromptFile(path);
    setPromptFileFound(true);
    setPromptFileContent(data?.result?.content || '');
    await ui.notify({ title: t('saved'), message: t('promptFileBootstrapped') });
    await load();
  };

  return (
    <div className="h-full p-4 md:p-6 flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl md:text-2xl font-semibold">{t('subagentsRuntime')}</h1>
        <button onClick={() => load()} className="px-3 py-1.5 rounded-lg bg-zinc-800 hover:bg-zinc-700 text-sm">{t('refresh')}</button>
      </div>

      <div className="border border-zinc-800 rounded-2xl bg-zinc-900/40 p-4 space-y-3">
        <div className="flex items-center justify-between gap-3">
          <div>
            <div className="text-xs text-zinc-400 uppercase tracking-wider">{t('agentTopology')}</div>
            <div className="text-sm text-zinc-500">{t('agentTopologyHint')}</div>
          </div>
          <div className="flex items-center gap-2 flex-wrap justify-end">
            {(['all', 'running', 'failed', 'local', 'remote'] as const).map((filter) => (
              <button
                key={filter}
                onClick={() => setTopologyFilter(filter)}
                className={`px-2 py-1 rounded text-[11px] ${
                  topologyFilter === filter ? 'bg-amber-500/20 text-amber-200 border border-amber-500/40' : 'bg-zinc-800 hover:bg-zinc-700 text-zinc-300 border border-zinc-700'
                }`}
              >
                {t(`topologyFilter.${filter}`)}
              </button>
            ))}
            {selectedTopologyBranch && (
              <button
                onClick={() => setSelectedTopologyBranch('')}
                className="px-2 py-1 rounded bg-zinc-800 hover:bg-zinc-700 text-[11px] text-zinc-200"
              >
                {t('clearFocus')}
              </button>
            )}
            <div className="text-xs text-zinc-500">
              {items.filter((item) => item.status === 'running').length} {t('runningTasks')}
            </div>
          </div>
        </div>
        <div className="overflow-x-auto overflow-y-hidden rounded-xl border border-zinc-800 bg-[radial-gradient(circle_at_top,_rgba(251,191,36,0.08),_transparent_24%),linear-gradient(180deg,rgba(24,24,27,0.95),rgba(9,9,11,0.98))]">
          <svg width={topologyGraph.width} height={topologyGraph.height} className="block">
            {topologyGraph.lines.map((line, idx) => (
              line.hidden ? null : (
              <line
                key={`line-${idx}`}
                x1={line.x1}
                y1={line.y1}
                x2={line.x2}
                y2={line.y2}
                stroke={line.highlighted ? 'rgba(251,191,36,0.9)' : line.dashed ? 'rgba(251,191,36,0.5)' : 'rgba(113,113,122,0.7)'}
                strokeWidth={line.highlighted ? '2.8' : '2'}
                strokeDasharray={line.dashed ? '6 6' : undefined}
                opacity={line.dimmed ? 0.22 : 1}
              />
              )
            ))}
            {topologyGraph.cards.map((card) => (
              card.hidden ? null : <GraphCard key={card.key} card={card} />
            ))}
          </svg>
        </div>
      </div>

      <div className="flex-1 min-h-0 grid grid-cols-1 xl:grid-cols-[360px_1fr] gap-4">
        <div className="border border-zinc-800 rounded-xl bg-zinc-900/40 overflow-hidden">
          <div className="px-3 py-2 border-b border-zinc-800 text-xs text-zinc-400 uppercase tracking-wider">{t('subagentsRuntime')}</div>
          <div className="overflow-y-auto max-h-[70vh]">
            {items.map((it) => (
              <button
                key={it.id}
                onClick={() => setSelectedId(it.id)}
                className={`w-full text-left px-3 py-2 border-b border-zinc-800/50 hover:bg-zinc-800/40 ${selectedId === it.id ? 'bg-indigo-500/15' : ''}`}
              >
                <div className="text-sm text-zinc-100 truncate">{it.id}</div>
                <div className="text-xs text-zinc-400 truncate">{it.status} · {it.agent_id || '-'} · {it.role || '-'}</div>
              </button>
            ))}
            {items.length === 0 && <div className="px-3 py-4 text-sm text-zinc-500">No subagents.</div>}
          </div>
        </div>

        <div className="space-y-4 min-h-0 overflow-y-auto">
          <div className="border border-zinc-800 rounded-xl bg-zinc-900/40 p-4 space-y-3">
            <div className="text-xs text-zinc-400 uppercase tracking-wider">{t('subagentDetail')}</div>
            {!selected && <div className="text-sm text-zinc-500">{t('selectTask')}</div>}
            {selected && (
              <>
                <div className="grid grid-cols-1 md:grid-cols-2 gap-3 text-xs">
                  <div><span className="text-zinc-400">ID:</span> {selected.id}</div>
                  <div><span className="text-zinc-400">Status:</span> {selected.status}</div>
                  <div><span className="text-zinc-400">Agent ID:</span> {selected.agent_id || '-'}</div>
                  <div><span className="text-zinc-400">Role:</span> {selected.role || '-'}</div>
                  <div className="md:col-span-2"><span className="text-zinc-400">Session:</span> {selected.session_key || '-'}</div>
                  <div className="md:col-span-2"><span className="text-zinc-400">Thread:</span> {selected.thread_id || '-'}</div>
                  <div className="md:col-span-2"><span className="text-zinc-400">Correlation:</span> {selected.correlation_id || '-'}</div>
                  <div className="md:col-span-2"><span className="text-zinc-400">Memory NS:</span> {selected.memory_ns || '-'}</div>
                  <div><span className="text-zinc-400">Retries:</span> {selected.retry_count || 0}/{selected.max_retries || 0}</div>
                  <div><span className="text-zinc-400">Timeout:</span> {selected.timeout_sec || 0}s</div>
                  <div><span className="text-zinc-400">Waiting Reply:</span> {selected.waiting_for_reply ? 'yes' : 'no'}</div>
                </div>
                <div className="text-xs text-zinc-400">Task</div>
                <pre className="text-xs bg-zinc-950 border border-zinc-800 rounded p-3 whitespace-pre-wrap break-words">{selected.task || '-'}</pre>
                <div className="text-xs text-zinc-400">Result</div>
                <pre className="text-xs bg-zinc-950 border border-zinc-800 rounded p-3 whitespace-pre-wrap break-words max-h-64 overflow-auto">{selected.result || '-'}</pre>
                <div className="flex items-center gap-2">
                  <button onClick={resume} className="px-3 py-1.5 text-xs rounded bg-emerald-700/70 hover:bg-emerald-600">{t('resume')}</button>
                  <button onClick={kill} className="px-3 py-1.5 text-xs rounded bg-red-700/70 hover:bg-red-600">{t('kill')}</button>
                </div>
                <div className="flex items-center gap-2">
                  <input
                    value={steerMessage}
                    onChange={(e) => setSteerMessage(e.target.value)}
                    placeholder={t('steerMessage')}
                    className="flex-1 px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded"
                  />
                  <button onClick={steer} className="px-3 py-1.5 text-xs rounded bg-indigo-700/70 hover:bg-indigo-600">{t('send')}</button>
                </div>
              </>
            )}
          </div>

          <div className="border border-zinc-800 rounded-xl bg-zinc-900/40 p-4 space-y-3">
            <div className="text-xs text-zinc-400 uppercase tracking-wider">{t('spawnSubagent')}</div>
            <textarea
              value={spawnTask}
              onChange={(e) => setSpawnTask(e.target.value)}
              placeholder="Task"
              className="w-full px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded min-h-[110px]"
            />
            <div className="grid grid-cols-1 md:grid-cols-3 gap-2">
              <input value={spawnAgentID} onChange={(e) => setSpawnAgentID(e.target.value)} placeholder="agent_id" className="px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded" />
              <input value={spawnRole} onChange={(e) => setSpawnRole(e.target.value)} placeholder="role" className="px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded" />
              <input value={spawnLabel} onChange={(e) => setSpawnLabel(e.target.value)} placeholder="label" className="px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded" />
            </div>
            <button onClick={spawn} className="px-3 py-1.5 text-xs rounded bg-indigo-700/80 hover:bg-indigo-600">{t('spawn')}</button>
          </div>

          <div className="border border-zinc-800 rounded-xl bg-zinc-900/40 p-4 space-y-3">
            <div className="flex items-center justify-between gap-3">
              <div className="text-xs text-zinc-400 uppercase tracking-wider">{t('agentRegistry')}</div>
              <button onClick={() => load()} className="px-2 py-1 text-[11px] rounded bg-zinc-800 hover:bg-zinc-700">{t('refresh')}</button>
            </div>
            <div className="border border-zinc-800 rounded overflow-hidden">
              {registryItems.map((item) => (
                <div key={item.agent_id || 'unknown'} className="px-3 py-2 border-b last:border-b-0 border-zinc-800/60 text-xs space-y-2">
                  <div className="text-zinc-100">{item.agent_id || '-'} · {item.role || '-'} · {item.enabled ? t('active') : t('paused')}</div>
                  <div className="text-zinc-400">{item.type || '-'} · {item.transport || 'local'} · {item.display_name || '-'}</div>
                  {(item.node_id || item.parent_agent_id || item.managed_by) && (
                    <div className="text-zinc-500 break-words">
                      {item.node_id ? `node=${item.node_id}` : ''}
                      {item.node_id && item.parent_agent_id ? ' · ' : ''}
                      {item.parent_agent_id ? `parent=${item.parent_agent_id}` : ''}
                      {(item.node_id || item.parent_agent_id) && item.managed_by ? ' · ' : ''}
                      {item.managed_by ? `source=${item.managed_by}` : ''}
                    </div>
                  )}
                  <div className="text-zinc-500 break-words">{item.system_prompt_file || '-'}</div>
                  <div className="text-zinc-500">{item.prompt_file_found ? t('promptFileReady') : t('promptFileMissing')}</div>
                  <div className="text-zinc-300 whitespace-pre-wrap break-words">{item.system_prompt || item.description || '-'}</div>
                  <div className="text-zinc-500 break-words">{(item.routing_keywords || []).join(', ') || '-'}</div>
                  <div className="flex items-center gap-2">
                    <button onClick={() => loadRegistryItem(item)} className="px-2 py-1 rounded bg-indigo-700/70 hover:bg-indigo-600 text-[11px]">{t('loadDraft')}</button>
                    {item.managed_by === 'config.json' && (
                      <button onClick={() => setRegistryEnabled(item, !item.enabled)} className="px-2 py-1 rounded bg-zinc-800 hover:bg-zinc-700 text-[11px]">
                        {item.enabled ? t('disableAgent') : t('enableAgent')}
                      </button>
                    )}
                    {item.managed_by === 'config.json' && item.agent_id !== 'main' && (
                      <button onClick={() => deleteRegistryItem(item)} className="px-2 py-1 rounded bg-red-700/70 hover:bg-red-600 text-[11px]">{t('delete')}</button>
                    )}
                  </div>
                </div>
              ))}
              {registryItems.length === 0 && <div className="px-3 py-4 text-sm text-zinc-500">{t('noRegistryAgents')}</div>}
            </div>
          </div>

          <div className="border border-zinc-800 rounded-xl bg-zinc-900/40 p-4 space-y-3">
            <div className="text-xs text-zinc-400 uppercase tracking-wider">{t('configSubagentDraft')}</div>
            <div className="grid grid-cols-1 md:grid-cols-3 gap-2">
              <input value={configAgentID} onChange={(e) => setConfigAgentID(e.target.value)} placeholder="agent_id" className="px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded" />
              <input value={configRole} onChange={(e) => setConfigRole(e.target.value)} placeholder="role" className="px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded" />
              <input value={configDisplayName} onChange={(e) => setConfigDisplayName(e.target.value)} placeholder="display_name" className="px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded" />
            </div>
            <textarea
              value={configSystemPrompt}
              onChange={(e) => setConfigSystemPrompt(e.target.value)}
              placeholder="system_prompt"
              className="w-full px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded min-h-[96px]"
            />
            <input value={configSystemPromptFile} onChange={(e) => setConfigSystemPromptFile(e.target.value)} placeholder="system_prompt_file (relative AGENT.md path)" className="w-full px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded" />
            <input value={configToolAllowlist} onChange={(e) => setConfigToolAllowlist(e.target.value)} placeholder="tool_allowlist (comma separated)" className="w-full px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded" />
            <input value={configRoutingKeywords} onChange={(e) => setConfigRoutingKeywords(e.target.value)} placeholder="routing_keywords (comma separated)" className="w-full px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded" />
            <button onClick={upsertConfigSubagent} className="px-3 py-1.5 text-xs rounded bg-amber-700/80 hover:bg-amber-600">{t('saveToConfig')}</button>
          </div>

          <div className="border border-zinc-800 rounded-xl bg-zinc-900/40 p-4 space-y-3">
            <div className="flex items-center justify-between gap-3">
              <div className="text-xs text-zinc-400 uppercase tracking-wider">{t('promptFileEditor')}</div>
              <div className="text-[11px] text-zinc-500">{promptFileFound ? t('promptFileReady') : t('promptFileMissing')}</div>
            </div>
            <input
              value={configSystemPromptFile}
              onChange={(e) => setConfigSystemPromptFile(e.target.value)}
              placeholder="agents/<agent_id>/AGENT.md"
              className="w-full px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded"
            />
            <textarea
              value={promptFileContent}
              onChange={(e) => setPromptFileContent(e.target.value)}
              placeholder={t('promptFileEditorPlaceholder')}
              className="w-full px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded min-h-[220px]"
            />
            <div className="flex items-center gap-2">
              <button onClick={bootstrapPromptFile} className="px-3 py-1.5 text-xs rounded bg-zinc-700 hover:bg-zinc-600">{t('bootstrapPromptFile')}</button>
              <button onClick={savePromptFile} className="px-3 py-1.5 text-xs rounded bg-emerald-700/80 hover:bg-emerald-600">{t('savePromptFile')}</button>
            </div>
          </div>

          <div className="border border-zinc-800 rounded-xl bg-zinc-900/40 p-4 space-y-3">
            <div className="text-xs text-zinc-400 uppercase tracking-wider">{t('dispatchAndWait')}</div>
            <textarea
              value={dispatchTask}
              onChange={(e) => setDispatchTask(e.target.value)}
              placeholder="Task"
              className="w-full px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded min-h-[110px]"
            />
            <div className="grid grid-cols-1 md:grid-cols-3 gap-2">
              <input value={dispatchAgentID} onChange={(e) => setDispatchAgentID(e.target.value)} placeholder="agent_id" className="px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded" />
              <input value={dispatchRole} onChange={(e) => setDispatchRole(e.target.value)} placeholder="role" className="px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded" />
              <input value={dispatchWaitTimeout} onChange={(e) => setDispatchWaitTimeout(e.target.value)} placeholder="wait_timeout_sec" className="px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded" />
            </div>
            <button onClick={dispatchAndWait} className="px-3 py-1.5 text-xs rounded bg-emerald-700/80 hover:bg-emerald-600">{t('dispatchAndWait')}</button>
            {dispatchReply && (
              <>
                <div className="text-xs text-zinc-400">{t('dispatchReply')}</div>
                <pre className="text-xs bg-zinc-950 border border-zinc-800 rounded p-3 whitespace-pre-wrap break-words">{JSON.stringify(dispatchReply, null, 2)}</pre>
              </>
            )}
            {dispatchMerged && (
              <>
                <div className="text-xs text-zinc-400">{t('mergedResult')}</div>
                <pre className="text-xs bg-zinc-950 border border-zinc-800 rounded p-3 whitespace-pre-wrap break-words max-h-64 overflow-auto">{dispatchMerged}</pre>
              </>
            )}
          </div>

          <div className="border border-zinc-800 rounded-xl bg-zinc-900/40 p-4 space-y-3">
            <div className="text-xs text-zinc-400 uppercase tracking-wider">{t('threadTrace')}</div>
            {!selected && <div className="text-sm text-zinc-500">{t('selectTask')}</div>}
            {selected && (
              <>
                <div className="grid grid-cols-1 md:grid-cols-2 gap-3 text-xs">
                  <div><span className="text-zinc-400">Thread:</span> {threadDetail?.thread_id || selected.thread_id || '-'}</div>
                  <div><span className="text-zinc-400">Owner:</span> {threadDetail?.owner || '-'}</div>
                  <div><span className="text-zinc-400">Status:</span> {threadDetail?.status || '-'}</div>
                  <div><span className="text-zinc-400">Participants:</span> {(threadDetail?.participants || []).join(', ') || '-'}</div>
                </div>
                <div className="text-xs text-zinc-400">{t('threadMessages')}</div>
                <div className="border border-zinc-800 rounded overflow-hidden">
                  {threadMessages.map((msg) => (
                    <div key={`${msg.message_id}-${msg.status}-${msg.created_at}`} className="px-3 py-2 border-b last:border-b-0 border-zinc-800/60 text-xs">
                      <div className="text-zinc-100">{msg.message_id} · {msg.type} · {msg.status}</div>
                      <div className="text-zinc-400">{msg.from_agent} → {msg.to_agent} · reply_to: {msg.reply_to || '-'}</div>
                      <div className="text-zinc-300 mt-1 whitespace-pre-wrap break-words">{msg.content || '-'}</div>
                    </div>
                  ))}
                  {threadMessages.length === 0 && <div className="px-3 py-4 text-sm text-zinc-500">No thread messages.</div>}
                </div>
              </>
            )}
          </div>

          <div className="border border-zinc-800 rounded-xl bg-zinc-900/40 p-4 space-y-3">
            <div className="text-xs text-zinc-400 uppercase tracking-wider">{t('inbox')}</div>
            {!selected && <div className="text-sm text-zinc-500">{t('selectTask')}</div>}
            {selected && (
              <>
                <div className="border border-zinc-800 rounded overflow-hidden">
                  {inboxMessages.map((msg) => (
                    <div key={`${msg.message_id}-${msg.status}`} className="px-3 py-2 border-b last:border-b-0 border-zinc-800/60 text-xs">
                      <div className="flex items-center justify-between gap-3">
                        <div className="text-zinc-100">{msg.message_id} · {msg.type} · {msg.status}</div>
                        {msg.message_id && (
                          <button onClick={() => ackMessage(msg.message_id || '')} className="px-2 py-1 rounded bg-zinc-800 hover:bg-zinc-700 text-[11px]">{t('ack')}</button>
                        )}
                      </div>
                      <div className="text-zinc-400">{msg.from_agent} → {msg.to_agent}</div>
                      <div className="text-zinc-300 mt-1 whitespace-pre-wrap break-words">{msg.content || '-'}</div>
                    </div>
                  ))}
                  {inboxMessages.length === 0 && <div className="px-3 py-4 text-sm text-zinc-500">No queued inbox messages.</div>}
                </div>
                <div className="grid grid-cols-1 md:grid-cols-[1fr_220px] gap-2">
                  <input value={replyMessage} onChange={(e) => setReplyMessage(e.target.value)} placeholder={t('message')} className="px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded" />
                  <input value={replyToMessageID} onChange={(e) => setReplyToMessageID(e.target.value)} placeholder="reply_to message_id" className="px-2 py-1 text-xs bg-zinc-900 border border-zinc-700 rounded" />
                </div>
                <div className="flex items-center gap-2">
                  <button onClick={sendMessage} className="px-3 py-1.5 text-xs rounded bg-indigo-700/70 hover:bg-indigo-600">{t('send')}</button>
                  <button onClick={replyToMessage} className="px-3 py-1.5 text-xs rounded bg-emerald-700/70 hover:bg-emerald-600">{t('reply')}</button>
                </div>
              </>
            )}
          </div>
        </div>
      </div>
    </div>
  );
};

export default Subagents;
