import React, { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import { useUI } from '../context/UIContext';
import { Activity, Server, Cpu, Network } from 'lucide-react';
import { SpaceParticles } from '../components/SpaceParticles';

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

type StreamItem = {
  kind?: 'event' | 'message' | string;
  at?: number;
  run_id?: string;
  agent_id?: string;
  event_type?: string;
  message?: string;
  retry_count?: number;
  message_id?: string;
  thread_id?: string;
  from_agent?: string;
  to_agent?: string;
  reply_to?: string;
  correlation_id?: string;
  message_type?: string;
  content?: string;
  status?: string;
  requires_reply?: boolean;
};

type RegistrySubagent = {
  agent_id?: string;
  enabled?: boolean;
  type?: string;
  transport?: string;
  node_id?: string;
  parent_agent_id?: string;
  managed_by?: string;
  notify_main_policy?: string;
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
  latestStatus: string;
  latestUpdated: number;
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
  scale: number;
  onClick?: () => void;
};

type GraphLineSpec = {
  path: string;
  dashed?: boolean;
  branch: string;
  highlighted?: boolean;
  dimmed?: boolean;
  hidden?: boolean;
};

type TopologyTooltipState = {
  title: string;
  subtitle: string;
  meta: string[];
  x: number;
  y: number;
  agentID?: string;
  transportType?: 'local' | 'remote';
} | null;

type StreamPreviewState = {
  task: SubagentTask | null;
  items: StreamItem[];
  taskID: string;
  loading?: boolean;
};

type TopologyDragState = {
  active: boolean;
  startX: number;
  startY: number;
  scrollLeft: number;
  scrollTop: number;
};

const cardWidth = 140;
const cardHeight = 140;
const clusterGap = 60;
const sectionGap = 160;
const topY = 96;
const mainY = 96;
const childStartY = 320;

function normalizeTitle(value?: string, fallback = '-'): string {
  const trimmed = `${value || ''}`.trim();
  return trimmed || fallback;
}

function summarizeTask(task?: string, label?: string): string {
  const text = normalizeTitle(label || task, '-');
  return text.length > 52 ? `${text.slice(0, 49)}...` : text;
}

function formatStreamTime(ts?: number): string {
  if (!ts) return '--:--:--';
  return new Date(ts).toLocaleTimeString([], { hour12: false });
}

function summarizePreviewText(value?: string, limit = 180): string {
  const compact = `${value || ''}`.replace(/\s+/g, ' ').trim();
  if (!compact) return '(empty)';
  return compact.length > limit ? `${compact.slice(0, limit - 3)}...` : compact;
}

function bezierCurve(x1: number, y1: number, x2: number, y2: number): string {
  const offset = Math.max(Math.abs(y2 - y1) * 0.5, 60);
  return `M ${x1} ${y1} C ${x1} ${y1 + offset} ${x2} ${y2 - offset} ${x2} ${y2}`;
}

function horizontalBezierCurve(x1: number, y1: number, x2: number, y2: number): string {
  const offset = Math.max(Math.abs(x2 - x1) * 0.5, 60);
  return `M ${x1} ${y1} C ${x1 + offset} ${y1} ${x2 - offset} ${y2} ${x2} ${y2}`;
}

function buildTaskStats(tasks: SubagentTask[]): Record<string, AgentTaskStats> {
  return tasks.reduce<Record<string, AgentTaskStats>>((acc, task) => {
    const agentID = normalizeTitle(task.agent_id, '');
    if (!agentID) return acc;
    if (!acc[agentID]) {
      acc[agentID] = { total: 0, running: 0, failed: 0, waiting: 0, latestStatus: '', latestUpdated: 0, active: [] };
    }
    const item = acc[agentID];
    item.total += 1;
    if (task.status === 'running') item.running += 1;
    if (task.waiting_for_reply) item.waiting += 1;
    const updatedAt = Math.max(task.updated || 0, task.created || 0);
    if (updatedAt >= item.latestUpdated) {
      item.latestUpdated = updatedAt;
      item.latestStatus = normalizeTitle(task.status, '');
      item.failed = task.status === 'failed' ? 1 : 0;
    }
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

function GraphCard({
  card,
  onHover,
  onLeave,
  onDragStart,
}: {
  card: GraphCardSpec;
  onHover: (card: GraphCardSpec, event: React.MouseEvent<HTMLDivElement>) => void;
  onLeave: () => void;
  onDragStart: (key: string, event: React.MouseEvent<HTMLDivElement>) => void;
}) {
  const isNode = card.kind === 'node';
  const Icon = isNode ? Server : Cpu;

  return (
    <foreignObject
      x={card.x}
      y={card.y}
      width={card.w}
      height={card.h}
      className="overflow-visible"
    >
      <div
        onMouseDown={(e) => onDragStart(card.key, e)}
        onClick={(e) => {
          if (e.defaultPrevented) return;
          card.onClick?.();
        }}
        onMouseEnter={(event) => onHover(card, event)}
        onMouseMove={(event) => onHover(card, event)}
        onMouseLeave={onLeave}
        className={`relative w-full h-full rounded-full flex flex-col items-center justify-center gap-1 transition-all duration-300 group ${card.highlighted
          ? 'scale-[1.05] z-10'
          : 'hover:scale-[1.02]'
          }`}
        style={{
          cursor: card.clickable ? 'pointer' : 'default',
          opacity: card.dimmed ? 0.3 : 1,
        }}
      >
        {/* Sleek Glass Node Background */}
        <div className={`absolute inset-0 rounded-full transition-all duration-300 backdrop-blur-md ${card.highlighted
          ? 'shadow-[0_0_30px_rgba(245,158,11,0.2)]'
          : 'shadow-xl shadow-black/60 group-hover:shadow-[0_0_20px_rgba(255,255,255,0.05)]'
          }`}>
          {/* Base dark glass */}
          <div className="absolute inset-0 rounded-full bg-gradient-to-b from-zinc-800/95 to-zinc-950/95" />

          {/* Subtle accent glow */}
          <div className={`absolute inset-0 rounded-full opacity-20 ${card.accent.replace('bg-', 'bg-gradient-to-br from-transparent to-')}`} />

          {/* Inner depth ring */}
          <div className="absolute inset-[1px] rounded-full border border-white/5" />

          {/* Border ring */}
          <div className={`absolute inset-0 rounded-full border-[1.5px] ${card.highlighted ? 'border-amber-500/80' : 'border-zinc-700/80 group-hover:border-zinc-500/80'
            }`} />
        </div>

        {/* Content */}
        <div className="relative z-10 flex flex-col items-center justify-center w-full px-4 text-center">
          <div className={`flex items-center justify-center w-10 h-10 mb-1 rounded-full bg-zinc-950/60 border border-zinc-700/50 shadow-inner backdrop-blur-sm`}>
            <Icon className={`w-5 h-5 ${card.accent.replace('bg-', 'text-')}`} />
          </div>

          <div className="w-full">
            <div className="text-[13px] font-bold text-zinc-100 truncate leading-tight drop-shadow-md">{card.title}</div>
            <div className="text-[10px] text-zinc-300/90 truncate mt-0.5 drop-shadow-sm">{card.subtitle}</div>
          </div>

          {card.online !== undefined && (
            <div className={`absolute top-6 right-6 w-2.5 h-2.5 rounded-full border border-zinc-900 ${card.online ? 'bg-emerald-500 shadow-[0_0_10px_rgba(16,185,129,0.8)]' : 'bg-red-500'}`} />
          )}
        </div>
      </div>
    </foreignObject>
  );
}

const Subagents: React.FC = () => {
  const { t } = useTranslation();
  const { q, nodeTrees } = useAppContext();
  const ui = useUI();

  const [items, setItems] = useState<SubagentTask[]>([]);
  const [selectedId, setSelectedId] = useState<string>('');
  const [selectedAgentID, setSelectedAgentID] = useState<string>('');
  const [spawnTask, setSpawnTask] = useState('');
  const [spawnAgentID, setSpawnAgentID] = useState('');
  const [spawnRole, setSpawnRole] = useState('');
  const [spawnLabel, setSpawnLabel] = useState('');
  const [steerMessage, setSteerMessage] = useState('');
  const [dispatchTask, setDispatchTask] = useState('');
  const [dispatchAgentID, setDispatchAgentID] = useState('');
  const [dispatchRole, setDispatchRole] = useState('');
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
  const [streamPreviewByAgent, setStreamPreviewByAgent] = useState<Record<string, StreamPreviewState>>({});
  const [selectedTopologyBranch, setSelectedTopologyBranch] = useState('');
  const [topologyFilter, setTopologyFilter] = useState<'all' | 'running' | 'failed' | 'local' | 'remote'>('all');
  const [topologyZoom, setTopologyZoom] = useState(0.9);
  const [topologyPan, setTopologyPan] = useState({ x: 0, y: 0 });
  const [nodeOverrides, setNodeOverrides] = useState<Record<string, { x: number, y: number }>>({});
  const [draggedNode, setDraggedNode] = useState<string | null>(null);
  const [topologyTooltip, setTopologyTooltip] = useState<TopologyTooltipState>(null);
  const [topologyDragging, setTopologyDragging] = useState(false);
  const topologyViewportRef = useRef<HTMLDivElement | null>(null);
  const topologyDragRef = useRef<{
    active: boolean;
    startX: number;
    startY: number;
    panX: number;
    panY: number;
  }>({
    active: false,
    startX: 0,
    startY: 0,
    panX: 0,
    panY: 0,
  });
  const nodeDragRef = useRef<{
    startX: number;
    startY: number;
    initialNodeX: number;
    initialNodeY: number;
  }>({ startX: 0, startY: 0, initialNodeX: 0, initialNodeY: 0 });
  const hasFittedRef = useRef(false);
  const streamPreviewLoadingRef = useRef<Record<string, string>>({});

  const apiPath = '/webui/api/subagents_runtime';
  const withAction = (action: string) => `${apiPath}${q}${q ? '&' : '?'}action=${encodeURIComponent(action)}`;

  const openAgentStream = (agentID: string, taskID = '', branch = '') => {
    if (branch) setSelectedTopologyBranch(branch);
    setSelectedAgentID(agentID);
    setSelectedId(taskID);
  };

  const load = async () => {
    try {
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
      if (registryItems.length === 0) {
        setSelectedAgentID('');
        setSelectedId('');
      } else {
        const nextAgentID = selectedAgentID && registryItems.find((x: RegistrySubagent) => x.agent_id === selectedAgentID)
          ? selectedAgentID
          : (registryItems[0]?.agent_id || '');
        setSelectedAgentID(nextAgentID);
        const nextTask = arr.find((x: SubagentTask) => x.agent_id === nextAgentID);
        setSelectedId(nextTask?.id || '');
      }
    } catch (e) {
      // Mock data for preview
      setItems([
        { id: 'task-1', status: 'running', agent_id: 'worker-1', role: 'worker', task: 'Process data stream', created: Date.now() }
      ]);
    }
  };

  useEffect(() => {
    load().catch(() => { });
  }, [q, selectedAgentID]);

  useEffect(() => {
    const timer = window.setInterval(() => {
      load().catch(() => { });
    }, 5000);
    return () => window.clearInterval(timer);
  }, [q, selectedAgentID]);

  const selected = useMemo(() => items.find((x) => x.id === selectedId) || null, [items, selectedId]);
  const parsedNodeTrees = useMemo<NodeTree[]>(() => {
    try {
      const parsed = JSON.parse(nodeTrees);
      return Array.isArray(parsed) ? parsed : [];
    } catch {
      return [];
    }
  }, [nodeTrees]);
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
    const scale = topologyZoom;
    const originX = 56;
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
    const localChildren = Array.isArray(localRoot.children) ? localRoot.children : [];
    const localBranchWidth = Math.max(cardWidth, localChildren.length * (cardWidth + clusterGap) - clusterGap);
    const localOriginX = originX;
    const localMainX = localOriginX + Math.max(0, (localBranchWidth - cardWidth) / 2);
    const localMainCenterX = localMainX + cardWidth / 2;
    const localMainCenterY = mainY + cardHeight / 2;
    const remoteClusters = remoteTrees.map((tree) => {
      const root = tree.root?.root;
      const children = Array.isArray(root?.children) ? root.children : [];
      return {
        tree,
        root,
        children,
        width: Math.max(cardWidth, children.length * (cardWidth + clusterGap) - clusterGap),
      };
    });
    const totalRemoteWidth = remoteClusters.reduce((sum, cluster, idx) => {
      return sum + cluster.width + (idx > 0 ? sectionGap : 0);
    }, 0);
    const width = Math.max(900, localOriginX * 2 + localBranchWidth + (remoteClusters.length > 0 ? sectionGap + totalRemoteWidth : 0));
    const height = childStartY + cardHeight + 40;
    const cards: GraphCardSpec[] = [];
    const lines: GraphLineSpec[] = [];
    const localBranch = 'local';
    const localBranchStats = {
      running: 0,
      failed: 0,
    };

    const localMainStats = taskStats[normalizeTitle(localRoot.agent_id, 'main')] || { total: 0, running: 0, failed: 0, waiting: 0, latestStatus: '', latestUpdated: 0, active: [] };
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
        `children=${localChildren.length + remoteClusters.length}`,
        `total=${localMainStats.total} running=${localMainStats.running}`,
        `waiting=${localMainStats.waiting} failed=${localMainStats.failed}`,
        `notify=${normalizeTitle(registryItems.find((item) => item.agent_id === localRoot.agent_id)?.notify_main_policy, 'final_only')}`,
        `transport=${normalizeTitle(localRoot.transport, 'local')} type=${normalizeTitle(localRoot.type, 'router')}`,
        localMainStats.active[0] ? `task: ${localMainStats.active[0].title}` : t('noLiveTasks'),
      ],
      accent: localMainStats.running > 0 ? 'bg-emerald-500' : localMainStats.latestStatus === 'failed' ? 'bg-red-500' : 'bg-amber-400',
      clickable: true,
      scale,
      onClick: () => {
        openAgentStream(normalizeTitle(localRoot.agent_id, 'main'), localMainTask?.id || '', localBranch);
      },
    };
    cards.push(localMainCard);

    localChildren.forEach((child, idx) => {
      const childX = localOriginX + idx * (cardWidth + clusterGap);
      const childY = childStartY;
      const stats = taskStats[normalizeTitle(child.agent_id, '')] || { total: 0, running: 0, failed: 0, waiting: 0, latestStatus: '', latestUpdated: 0, active: [] };
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
          `notify=${normalizeTitle(registryItems.find((item) => item.agent_id === child.agent_id)?.notify_main_policy, 'final_only')}`,
          `transport=${normalizeTitle(child.transport, 'local')} type=${normalizeTitle(child.type, 'worker')}`,
          stats.active[0] ? `task: ${stats.active[0].title}` : task ? `last: ${summarizeTask(task.task, task.label)}` : t('noLiveTasks'),
        ],
        accent: stats.running > 0 ? 'bg-emerald-500' : stats.latestStatus === 'failed' ? 'bg-red-500' : 'bg-sky-400',
        clickable: true,
        scale,
        onClick: () => {
          openAgentStream(normalizeTitle(child.agent_id, ''), task?.id || '', localBranch);
        },
      });
      lines.push({
        path: bezierCurve(localMainCard.x + cardWidth / 2, localMainCard.y + cardHeight / 2, childX + cardWidth / 2, childY + cardHeight / 2),
        branch: localBranch,
      });
    });

    let remoteOffsetX = localOriginX + localBranchWidth + sectionGap;
    remoteClusters.forEach((cluster, treeIndex) => {
      const { tree, root: treeRoot, children } = cluster;
      const branch = `node:${normalizeTitle(tree.node_id, `remote-${treeIndex}`)}`;
      const rootX = remoteOffsetX + Math.max(0, (cluster.width - cardWidth) / 2);
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
          `status=${tree.online ? t('online') : t('offline')}`,
          `transport=${normalizeTitle(treeRoot.transport, 'node')} type=${normalizeTitle(treeRoot.type, 'router')}`,
          `source=${normalizeTitle(treeRoot.managed_by, tree.source || '-')}`,
          t('remoteTasksUnavailable'),
        ],
        accent: tree.online ? 'bg-fuchsia-400' : 'bg-zinc-500',
        clickable: true,
        scale,
        onClick: () => {
          openAgentStream(normalizeTitle(treeRoot.agent_id, ''), '', branch);
        },
      };
      cards.push(rootCard);
      lines.push({
        path: horizontalBezierCurve(localMainCard.x + cardWidth / 2, localMainCard.y + cardHeight / 2, rootCard.x + cardWidth / 2, rootCard.y + cardHeight / 2),
        dashed: true,
        branch,
      });
      children.forEach((child, idx) => {
        const childX = remoteOffsetX + idx * (cardWidth + clusterGap);
        const childY = childStartY;
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
          scale,
          onClick: () => {
            openAgentStream(normalizeTitle(child.agent_id, ''), '', branch);
          },
        });
        lines.push({
          path: bezierCurve(rootCard.x + cardWidth / 2, rootCard.y + cardHeight / 2, childX + cardWidth / 2, childY + cardHeight / 2),
          branch,
        });
      });
      remoteOffsetX += cluster.width + sectionGap;
    });

    const highlightedBranch = selectedTopologyBranch.trim();
    const branchFilters = new Map<string, boolean>();
    branchFilters.set(localBranch, topologyFilter === 'all' || topologyFilter === 'local' || (topologyFilter === 'running' && localBranchStats.running > 0) || (topologyFilter === 'failed' && localBranchStats.failed > 0));
    remoteTrees.forEach((tree, treeIndex) => {
      const branch = `node:${normalizeTitle(tree.node_id, `remote-${treeIndex}`)}`;
      branchFilters.set(branch, topologyFilter === 'all' || topologyFilter === 'remote');
    });
    const decoratedCards = cards.map((card) => {
      const override = nodeOverrides[card.key];
      return {
        ...card,
        x: override ? override.x : card.x,
        y: override ? override.y : card.y,
        hidden: branchFilters.get(card.branch) === false,
        highlighted: !highlightedBranch || card.branch === highlightedBranch,
        dimmed: branchFilters.get(card.branch) === false ? true : !!highlightedBranch && card.branch !== highlightedBranch,
      };
    });

    // Recalculate lines based on potentially overridden card positions
    const recalculatedLines: GraphLineSpec[] = [];

    // Helper to find a card's current position
    const getCardPos = (key: string) => {
      const c = decoratedCards.find(c => c.key === key);
      return c ? { cx: c.x + cardWidth / 2, cy: c.y + cardHeight / 2 } : null;
    };

    const localMainPos = getCardPos('agent-main');

    localChildren.forEach((child, idx) => {
      const childKey = `local-child-${child.agent_id || idx}`;
      const childPos = getCardPos(childKey);
      if (localMainPos && childPos) {
        recalculatedLines.push({
          path: bezierCurve(localMainPos.cx, localMainPos.cy, childPos.cx, childPos.cy),
          branch: localBranch,
        });
      }
    });

    remoteClusters.forEach((cluster, treeIndex) => {
      const branch = `node:${normalizeTitle(cluster.tree.node_id, `remote-${treeIndex}`)}`;
      const remoteRootKey = `remote-root-${cluster.tree.node_id || treeIndex}`;
      const remoteRootPos = getCardPos(remoteRootKey);

      if (localMainPos && remoteRootPos) {
        recalculatedLines.push({
          path: horizontalBezierCurve(localMainPos.cx, localMainPos.cy, remoteRootPos.cx, remoteRootPos.cy),
          dashed: true,
          branch,
        });
      }

      cluster.children.forEach((child, idx) => {
        const childKey = `remote-child-${cluster.tree.node_id || treeIndex}-${child.agent_id || idx}`;
        const childPos = getCardPos(childKey);
        if (remoteRootPos && childPos) {
          recalculatedLines.push({
            path: bezierCurve(remoteRootPos.cx, remoteRootPos.cy, childPos.cx, childPos.cy),
            branch,
          });
        }
      });
    });

    const decoratedLines = recalculatedLines.map((line) => ({
      ...line,
      hidden: branchFilters.get(line.branch) === false,
      highlighted: !highlightedBranch || line.branch === highlightedBranch,
      dimmed: branchFilters.get(line.branch) === false ? true : !!highlightedBranch && line.branch !== highlightedBranch,
    }));

    return { width, height, cards: decoratedCards, lines: decoratedLines };
  }, [parsedNodeTrees, registryItems, taskStats, recentTaskByAgent, selectedTopologyBranch, topologyFilter, t, topologyZoom, nodeOverrides]);

  const fitView = () => {
    const viewport = topologyViewportRef.current;
    if (!viewport || !topologyGraph.width) return;
    const availableW = viewport.clientWidth;
    const availableH = viewport.clientHeight;

    const fitted = Math.min(1.15, Math.max(0.2, (availableW - 48) / topologyGraph.width));

    setTopologyZoom(fitted);
    setTopologyPan({
      x: (availableW - topologyGraph.width * fitted) / 2,
      y: Math.max(24, (availableH - topologyGraph.height * fitted) / 2)
    });
  };

  useEffect(() => {
    if (!hasFittedRef.current && topologyGraph.width > 0) {
      fitView();
      hasFittedRef.current = true;
    }
  }, [topologyGraph.width]);

  useEffect(() => {
    const viewport = topologyViewportRef.current;
    if (!viewport) return;

    const handleWheel = (e: WheelEvent) => {
      if (!(e.ctrlKey || e.metaKey)) {
        return;
      }
      e.preventDefault();

      const rect = viewport.getBoundingClientRect();
      const mouseX = e.clientX - rect.left;
      const mouseY = e.clientY - rect.top;

      setTopologyZoom(prevZoom => {
        const zoomSensitivity = 0.002;
        const delta = -e.deltaY * zoomSensitivity;
        const newZoom = Math.min(Math.max(0.1, prevZoom * (1 + delta)), 4);

        setTopologyPan(prevPan => {
          const scaleRatio = newZoom / prevZoom;
          const newPanX = mouseX - (mouseX - prevPan.x) * scaleRatio;
          const newPanY = mouseY - (mouseY - prevPan.y) * scaleRatio;
          return { x: newPanX, y: newPanY };
        });

        return newZoom;
      });
    };

    viewport.addEventListener('wheel', handleWheel, { passive: false });
    return () => viewport.removeEventListener('wheel', handleWheel);
  }, []);

  const handleTopologyHover = (card: GraphCardSpec, event: React.MouseEvent<HTMLDivElement>) => {
    const tooltipWidth = 360;
    const tooltipHeight = 420;
    let x = event.clientX + 14;
    let y = event.clientY + 14;

    if (x + tooltipWidth > window.innerWidth) {
      x = event.clientX - tooltipWidth - 14;
    }
    if (y + tooltipHeight > window.innerHeight) {
      y = event.clientY - tooltipHeight - 14;
    }

    setTopologyTooltip({
      title: card.title,
      subtitle: card.subtitle,
      meta: card.meta,
      x,
      y,
      agentID: card.agentID,
      transportType: card.transportType,
    });
  };

  const clearTopologyTooltip = () => setTopologyTooltip(null);

  const handleNodeDragStart = (key: string, event: React.MouseEvent<HTMLDivElement>) => {
    if (event.button !== 0) return;
    event.stopPropagation();

    const card = topologyGraph.cards.find(c => c.key === key);
    if (!card) return;

    setDraggedNode(key);
    nodeDragRef.current = {
      startX: event.clientX,
      startY: event.clientY,
      initialNodeX: card.x,
      initialNodeY: card.y,
    };
    clearTopologyTooltip();
  };

  const startTopologyDrag = (event: React.MouseEvent<HTMLDivElement>) => {
    if (event.button !== 0) return;
    topologyDragRef.current = {
      active: true,
      startX: event.clientX,
      startY: event.clientY,
      panX: topologyPan.x,
      panY: topologyPan.y,
    };
    setTopologyDragging(true);
    clearTopologyTooltip();
  };

  const moveTopologyDrag = (event: React.MouseEvent<HTMLDivElement>) => {
    if (draggedNode) {
      const deltaX = (event.clientX - nodeDragRef.current.startX) / topologyZoom;
      const deltaY = (event.clientY - nodeDragRef.current.startY) / topologyZoom;

      setNodeOverrides(prev => ({
        ...prev,
        [draggedNode]: {
          x: nodeDragRef.current.initialNodeX + deltaX,
          y: nodeDragRef.current.initialNodeY + deltaY,
        }
      }));
      return;
    }

    if (!topologyDragRef.current.active) return;
    const deltaX = event.clientX - topologyDragRef.current.startX;
    const deltaY = event.clientY - topologyDragRef.current.startY;
    setTopologyPan({
      x: topologyDragRef.current.panX + deltaX,
      y: topologyDragRef.current.panY + deltaY,
    });
  };

  const stopTopologyDrag = () => {
    if (draggedNode) {
      setDraggedNode(null);
    }
    if (topologyDragRef.current.active) {
      topologyDragRef.current.active = false;
      setTopologyDragging(false);
    }
  };

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
    try {
      const [threadRes, inboxRes] = await Promise.all([
        callAction({ action: 'thread', id: task.id, limit: 50 }),
        callAction({ action: 'inbox', id: task.id, limit: 50 }),
      ]);
      setThreadDetail(threadRes?.result?.thread || null);
      setThreadMessages(Array.isArray(threadRes?.result?.messages) ? threadRes.result.messages : []);
      setInboxMessages(Array.isArray(inboxRes?.result?.messages) ? inboxRes.result.messages : []);
    } catch (e) { }
  };

  useEffect(() => {
    loadThreadAndInbox(selected).catch(() => { });
  }, [selectedId, q, items]);

  const loadStreamPreview = async (agentID: string, task: SubagentTask | null) => {
    const taskID = task?.id || '';
    if (!agentID) return;
    if (streamPreviewLoadingRef.current[agentID] === taskID) return;
    const existing = streamPreviewByAgent[agentID];
    if (existing && existing.taskID === taskID && !existing.loading) return;

    streamPreviewLoadingRef.current[agentID] = taskID;
    setStreamPreviewByAgent((prev) => ({
      ...prev,
      [agentID]: {
        task: task || null,
        items: prev[agentID]?.items || [],
        taskID,
        loading: !!taskID,
      },
    }));

    if (!taskID) {
      delete streamPreviewLoadingRef.current[agentID];
      setStreamPreviewByAgent((prev) => ({
        ...prev,
        [agentID]: { task: null, items: [], taskID: '', loading: false },
      }));
      return;
    }

    try {
      const streamRes = await callAction({ action: 'stream', id: taskID, limit: 12 });
      delete streamPreviewLoadingRef.current[agentID];
      setStreamPreviewByAgent((prev) => ({
        ...prev,
        [agentID]: {
          task: streamRes?.result?.task || task,
          items: Array.isArray(streamRes?.result?.items) ? streamRes.result.items : [],
          taskID,
          loading: false,
        },
      }));
    } catch {
      delete streamPreviewLoadingRef.current[agentID];
      setStreamPreviewByAgent((prev) => ({
        ...prev,
        [agentID]: { task: task || null, items: [], taskID, loading: false },
      }));
    }
  };

  useEffect(() => {
    if (!topologyTooltip?.agentID || topologyTooltip.transportType !== 'local') return;
    const latestTask = recentTaskByAgent[topologyTooltip.agentID] || null;
    loadStreamPreview(topologyTooltip.agentID, latestTask).catch(() => { });
  }, [topologyTooltip?.agentID, topologyTooltip?.transportType, recentTaskByAgent, q]);

  return (
    <div className="h-full p-4 md:p-6 xl:p-8 flex flex-col gap-4">
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <h1 className="text-xl md:text-2xl font-semibold">{t('subagentsRuntime')}</h1>
        <button onClick={() => load()} className="brand-button px-3 py-1.5 rounded-xl text-sm text-white">{t('refresh')}</button>
      </div>

      <div className="flex-1 min-h-0 brand-card border border-zinc-800 p-4 flex flex-col gap-3">
        <div className="flex items-center justify-between gap-3 flex-wrap">
          <div>
            <div className="text-xs text-zinc-400 uppercase tracking-wider">{t('agentTopology')}</div>
            <div className="text-sm text-zinc-500">{t('agentTopologyHint')}</div>
          </div>
          <div className="flex items-center gap-2 flex-wrap justify-end">
            {(['all', 'running', 'failed', 'local', 'remote'] as const).map((filter) => (
              <button
                key={filter}
                onClick={() => setTopologyFilter(filter)}
                className={`px-2 py-1 rounded-xl text-[11px] ${topologyFilter === filter ? 'control-chip-active' : 'control-chip'
                  }`}
              >
                {t(`topologyFilter.${filter}`)}
              </button>
            ))}
            {selectedTopologyBranch && (
              <button
                onClick={() => setSelectedTopologyBranch('')}
                className="px-2 py-1 rounded-xl text-[11px] control-chip"
              >
                {t('clearFocus')}
              </button>
            )}
            <div className="control-chip-group flex items-center gap-1 rounded-xl px-1 py-1">
              <button
                onClick={() => {
                  const newZoom = Math.max(0.1, Number((topologyZoom - 0.1).toFixed(2)));
                  const viewport = topologyViewportRef.current;
                  if (viewport) {
                    const rect = viewport.getBoundingClientRect();
                    const mouseX = rect.width / 2;
                    const mouseY = rect.height / 2;
                    const scaleRatio = newZoom / topologyZoom;
                    setTopologyPan(prev => ({
                      x: mouseX - (mouseX - prev.x) * scaleRatio,
                      y: mouseY - (mouseY - prev.y) * scaleRatio
                    }));
                  }
                  setTopologyZoom(newZoom);
                }}
                className="px-2 py-1 rounded-lg text-[11px] control-chip"
              >
                {t('zoomOut')}
              </button>
              <button
                onClick={fitView}
                className="px-2 py-1 rounded-lg text-[11px] control-chip"
              >
                {t('fitView')}
              </button>
              <button
                onClick={() => {
                  const newZoom = 1;
                  const viewport = topologyViewportRef.current;
                  if (viewport) {
                    const rect = viewport.getBoundingClientRect();
                    const mouseX = rect.width / 2;
                    const mouseY = rect.height / 2;
                    const scaleRatio = newZoom / topologyZoom;
                    setTopologyPan(prev => ({
                      x: mouseX - (mouseX - prev.x) * scaleRatio,
                      y: mouseY - (mouseY - prev.y) * scaleRatio
                    }));
                  }
                  setTopologyZoom(newZoom);
                }}
                className="px-2 py-1 rounded-lg text-[11px] control-chip"
              >
                100%
              </button>
              <button
                onClick={() => {
                  const newZoom = Math.min(4, Number((topologyZoom + 0.1).toFixed(2)));
                  const viewport = topologyViewportRef.current;
                  if (viewport) {
                    const rect = viewport.getBoundingClientRect();
                    const mouseX = rect.width / 2;
                    const mouseY = rect.height / 2;
                    const scaleRatio = newZoom / topologyZoom;
                    setTopologyPan(prev => ({
                      x: mouseX - (mouseX - prev.x) * scaleRatio,
                      y: mouseY - (mouseY - prev.y) * scaleRatio
                    }));
                  }
                  setTopologyZoom(newZoom);
                }}
                className="px-2 py-1 rounded-lg text-[11px] control-chip"
              >
                {t('zoomIn')}
              </button>
            </div>
            <div className="text-xs text-zinc-400">
              {Math.round(topologyZoom * 100)}% · {items.filter((item) => item.status === 'running').length} {t('runningTasks')}
            </div>
          </div>
        </div>
        <div
          ref={topologyViewportRef}
          onMouseDown={startTopologyDrag}
          onMouseMove={moveTopologyDrag}
          onMouseUp={stopTopologyDrag}
          onMouseLeave={() => {
            stopTopologyDrag();
            clearTopologyTooltip();
          }}
          className="radius-canvas relative flex-1 min-h-[420px] sm:min-h-[560px] xl:min-h-[760px] overflow-hidden border border-zinc-800 bg-zinc-950/80"
          style={{ cursor: topologyDragging ? 'grabbing' : 'grab' }}
        >
          <SpaceParticles />
          <div className="absolute inset-0 z-10">
            <div
              style={{
                transform: `translate(${topologyPan.x}px, ${topologyPan.y}px) scale(${topologyZoom})`,
                transformOrigin: '0 0',
                width: topologyGraph.width,
                height: topologyGraph.height,
                transition: topologyDragging || draggedNode ? 'none' : 'transform 0.1s ease-out',
              }}
              className="relative will-change-transform"
            >
              <svg
                width={topologyGraph.width}
                height={topologyGraph.height}
                className="block absolute top-0 left-0 overflow-visible"
              >
                <style>
                  {`
                    @keyframes flow {
                      from { stroke-dashoffset: 24; }
                      to { stroke-dashoffset: 0; }
                    }
                    .animate-flow {
                      animation: flow 1s linear infinite;
                    }
                    .animate-flow-fast {
                      animation: flow 0.5s linear infinite;
                    }
                  `}
                </style>
                {topologyGraph.lines.map((line, idx) => (
                  line.hidden ? null : (
                    <g key={`line-${idx}`}>
                      {/* Faint energy track */}
                      <path
                        d={line.path}
                        fill="none"
                        stroke={line.highlighted ? 'rgba(245,158,11,0.15)' : 'rgba(161,161,170,0.05)'}
                        strokeWidth={line.highlighted ? '6' : '2'}
                        strokeLinecap="round"
                        className="transition-all duration-300"
                      />
                      {/* Flowing light particles */}
                      <path
                        d={line.path}
                        fill="none"
                        stroke={line.highlighted ? 'rgba(245,158,11,0.9)' : 'rgba(161,161,170,0.5)'}
                        strokeWidth={line.highlighted ? '2.5' : '1.5'}
                        strokeDasharray={line.highlighted ? "6 18" : "4 20"}
                        className={line.highlighted ? "animate-flow-fast" : "animate-flow"}
                        strokeLinecap="round"
                        opacity={line.dimmed ? 0.1 : 1}
                      />
                    </g>
                  )
                ))}
                {topologyGraph.cards.map((card) => (
                  card.hidden ? null : <GraphCard key={card.key} card={card} onHover={handleTopologyHover} onLeave={clearTopologyTooltip} onDragStart={handleNodeDragStart} />
                ))}
              </svg>
            </div>
          </div>
          {topologyTooltip && (
            <div
              className="pointer-events-none fixed z-50 w-[360px] max-w-[min(360px,calc(100vw-24px))] max-h-[min(70vh,560px)] overflow-y-auto brand-card-subtle border border-zinc-700/80 p-4 shadow-2xl shadow-black/50 backdrop-blur-md transition-opacity duration-200"
              style={{ left: topologyTooltip.x, top: topologyTooltip.y }}
            >
              <div className="flex items-center gap-2 mb-2">
                <div className="w-2 h-2 rounded-full bg-amber-500" />
                <div className="text-sm font-semibold text-zinc-100">{topologyTooltip.title}</div>
              </div>
              <div className="text-xs text-zinc-400 mb-3 pb-3 border-b border-zinc-800/60">{topologyTooltip.subtitle}</div>
              <div className="space-y-1.5">
                {topologyTooltip.meta.map((line, idx) => {
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
              {topologyTooltip.transportType === 'local' && topologyTooltip.agentID && (
                <div className="mt-4 pt-4 border-t border-zinc-800/60 space-y-3">
                  <div className="text-[11px] text-zinc-500 uppercase tracking-wider">{t('internalStream')}</div>
                  {streamPreviewByAgent[topologyTooltip.agentID]?.loading ? (
                    <div className="text-xs text-zinc-400">Loading internal stream...</div>
                  ) : streamPreviewByAgent[topologyTooltip.agentID]?.task ? (
                    <>
                      <div className="brand-card-subtle border border-zinc-800 p-3 space-y-1.5">
                        <div className="text-xs text-zinc-300">run={streamPreviewByAgent[topologyTooltip.agentID]?.task?.id || '-'}</div>
                        <div className="text-xs text-zinc-400">
                          status={streamPreviewByAgent[topologyTooltip.agentID]?.task?.status || '-'} · thread={streamPreviewByAgent[topologyTooltip.agentID]?.task?.thread_id || '-'}
                        </div>
                      </div>
                      {streamPreviewByAgent[topologyTooltip.agentID]?.items?.length ? (
                        streamPreviewByAgent[topologyTooltip.agentID].items.slice(-3).reverse().map((item, idx) => (
                          <div key={`${item.kind || 'item'}-${item.at || 0}-${idx}`} className="brand-card-subtle border border-zinc-800 p-3 space-y-2">
                            <div className="flex items-center justify-between gap-2">
                              <div className="text-xs font-medium text-zinc-200">
                                {item.kind === 'event'
                                  ? `${item.event_type || 'event'}${item.status ? ` · ${item.status}` : ''}`
                                  : `${item.from_agent || '-'} -> ${item.to_agent || '-'} · ${item.message_type || 'message'}`}
                              </div>
                              <div className="text-[11px] text-zinc-500">{formatStreamTime(item.at)}</div>
                            </div>
                            <div className="text-xs text-zinc-300 leading-5">
                              {summarizePreviewText(item.kind === 'event' ? (item.message || '(no event message)') : (item.content || '(empty message)'))}
                            </div>
                          </div>
                        ))
                      ) : (
                        <div className="text-xs text-zinc-400">No internal stream events yet.</div>
                      )}
                    </>
                  ) : (
                    <div className="text-xs text-zinc-400">No persisted run for this agent yet.</div>
                  )}
                </div>
              )}
            </div>
          )}
        </div>
      </div>

    </div>
  );
};

export default Subagents;
