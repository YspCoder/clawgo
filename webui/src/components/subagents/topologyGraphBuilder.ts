import type { TFunction } from 'i18next';
import type { GraphCardSpec, GraphLineSpec } from './topologyTypes';
import type { NodeTree, RegistrySubagent, SubagentTask } from './subagentTypes';
import {
  bezierCurve,
  formatRuntimeTimestamp,
  horizontalBezierCurve,
  normalizeTitle,
  summarizeTask,
  type AgentTaskStats,
} from './topologyUtils';

const CARD_WIDTH = 140;
const CARD_HEIGHT = 140;
const CLUSTER_GAP = 60;
const SECTION_GAP = 160;
const MAIN_Y = 96;
const CHILD_START_Y = 320;
const ORIGIN_X = 56;

type NodeOverrideMap = Record<string, { x: number; y: number }>;
type P2PSessionLike = {
  node?: string;
  status?: string;
  retry_count?: number;
  last_ready_at?: string;
  last_error?: string;
};
type DispatchLike = {
  node?: string;
  used_transport?: string;
  fallback_from?: string;
};

type BuildTopologyGraphParams = {
  nodeOverrides: NodeOverrideMap;
  nodeP2PTransport?: string;
  onOpenAgentStream: (agentID: string, taskID?: string, branch?: string) => void;
  parsedNodeTrees: NodeTree[];
  p2pSessionByNode: Record<string, P2PSessionLike>;
  recentDispatchByNode: Record<string, DispatchLike>;
  recentTaskByAgent: Record<string, SubagentTask>;
  registryItems: RegistrySubagent[];
  selectedTopologyBranch: string;
  t: TFunction;
  taskStats: Record<string, AgentTaskStats>;
  topologyFilter: 'all' | 'running' | 'failed' | 'local' | 'remote';
  topologyZoom: number;
};

type RemoteCluster = {
  tree: NodeTree;
  root?: NodeTree['root'] extends { root?: infer R } ? R : never;
  children: NonNullable<NonNullable<NodeTree['root']>['root']>['children'] extends infer C ? Extract<C, unknown[]> : never;
  width: number;
};

function buildLocalFallbackTree(registryItems: RegistrySubagent[]) {
  return {
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
}

function buildBranchFilters(
  localBranch: string,
  localBranchStats: { running: number; failed: number },
  remoteTrees: NodeTree[],
  topologyFilter: BuildTopologyGraphParams['topologyFilter'],
) {
  const branchFilters = new Map<string, boolean>();
  branchFilters.set(
    localBranch,
    topologyFilter === 'all'
      || topologyFilter === 'local'
      || (topologyFilter === 'running' && localBranchStats.running > 0)
      || (topologyFilter === 'failed' && localBranchStats.failed > 0),
  );
  remoteTrees.forEach((tree, treeIndex) => {
    const branch = `node:${normalizeTitle(tree.node_id, `remote-${treeIndex}`)}`;
    branchFilters.set(branch, topologyFilter === 'all' || topologyFilter === 'remote');
  });
  return branchFilters;
}

function decorateGraph(
  cards: GraphCardSpec[],
  remoteClusters: RemoteCluster[],
  localChildren: Array<{ agent_id?: string }>,
  localBranch: string,
  selectedTopologyBranch: string,
  nodeOverrides: NodeOverrideMap,
  branchFilters: Map<string, boolean>,
) {
  const highlightedBranch = selectedTopologyBranch.trim();
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

  const getCardPos = (key: string) => {
    const card = decoratedCards.find((item) => item.key === key);
    return card ? { cx: card.x + CARD_WIDTH / 2, cy: card.y + CARD_HEIGHT / 2 } : null;
  };

  const localMainPos = getCardPos('agent-main');
  const recalculatedLines: GraphLineSpec[] = [];

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

  return { cards: decoratedCards, lines: decoratedLines };
}

export function buildTopologyGraph({
  nodeOverrides,
  nodeP2PTransport,
  onOpenAgentStream,
  parsedNodeTrees,
  p2pSessionByNode,
  recentDispatchByNode,
  recentTaskByAgent,
  registryItems,
  selectedTopologyBranch,
  t,
  taskStats,
  topologyFilter,
  topologyZoom,
}: BuildTopologyGraphParams) {
  const scale = topologyZoom;
  const localTree = parsedNodeTrees.find((tree) => normalizeTitle(tree.node_id, '') === 'local') || null;
  const remoteTrees = parsedNodeTrees.filter((tree) => normalizeTitle(tree.node_id, '') !== 'local');
  const localRoot = localTree?.root?.root || buildLocalFallbackTree(registryItems);
  const localChildren = Array.isArray(localRoot.children) ? localRoot.children : [];
  const localBranchWidth = Math.max(CARD_WIDTH, localChildren.length * (CARD_WIDTH + CLUSTER_GAP) - CLUSTER_GAP);
  const localMainX = ORIGIN_X + Math.max(0, (localBranchWidth - CARD_WIDTH) / 2);

  const remoteClusters: RemoteCluster[] = remoteTrees.map((tree) => {
    const root = tree.root?.root;
    const children = Array.isArray(root?.children) ? root.children : [];
    return {
      tree,
      root,
      children,
      width: Math.max(CARD_WIDTH, children.length * (CARD_WIDTH + CLUSTER_GAP) - CLUSTER_GAP),
    };
  });

  const totalRemoteWidth = remoteClusters.reduce((sum, cluster, idx) => {
    return sum + cluster.width + (idx > 0 ? SECTION_GAP : 0);
  }, 0);
  const width = Math.max(900, ORIGIN_X * 2 + localBranchWidth + (remoteClusters.length > 0 ? SECTION_GAP + totalRemoteWidth : 0));
  const height = CHILD_START_Y + CARD_HEIGHT + 40;
  const cards: GraphCardSpec[] = [];
  const localBranch = 'local';
  const localBranchStats = { running: 0, failed: 0 };

  const emptyStats: AgentTaskStats = { total: 0, running: 0, failed: 0, waiting: 0, latestStatus: '', latestUpdated: 0, active: [] };
  const localMainStats = taskStats[normalizeTitle(localRoot.agent_id, 'main')] || emptyStats;
  const localMainTask = recentTaskByAgent[normalizeTitle(localRoot.agent_id, 'main')];
  const localMainRegistry = registryItems.find((item) => item.agent_id === localRoot.agent_id);
  localBranchStats.running += localMainStats.running;
  localBranchStats.failed += localMainStats.failed;

  cards.push({
    key: 'agent-main',
    branch: localBranch,
    agentID: normalizeTitle(localRoot.agent_id, 'main'),
    transportType: 'local',
    kind: 'agent',
    x: localMainX,
    y: MAIN_Y,
    w: CARD_WIDTH,
    h: CARD_HEIGHT,
    title: normalizeTitle(localRoot.display_name, 'Main Agent'),
    subtitle: `${normalizeTitle(localRoot.agent_id, 'main')} · ${normalizeTitle(localRoot.role, '-')}`,
    meta: [
      `children=${localChildren.length + remoteClusters.length}`,
      `total=${localMainStats.total} running=${localMainStats.running}`,
      `waiting=${localMainStats.waiting} failed=${localMainStats.failed}`,
      `notify=${normalizeTitle(localMainRegistry?.notify_main_policy, 'final_only')}`,
      `transport=${normalizeTitle(localRoot.transport, 'local')} type=${normalizeTitle(localRoot.type, 'router')}`,
      `tools=${normalizeTitle(localMainRegistry?.tool_visibility?.mode, 'allowlist')} visible=${localMainRegistry?.tool_visibility?.effective_tool_count ?? 0} inherited=${localMainRegistry?.tool_visibility?.inherited_tool_count ?? 0}`,
      (localMainRegistry?.inherited_tools || []).length ? `inherits: ${(localMainRegistry?.inherited_tools || []).join(', ')}` : 'inherits: -',
      localMainStats.active[0] ? `task: ${localMainStats.active[0].title}` : t('noLiveTasks'),
    ],
    accentTone: localMainStats.running > 0 ? 'success' : localMainStats.latestStatus === 'failed' ? 'danger' : 'warning',
    clickable: true,
    scale,
    onClick: () => onOpenAgentStream(normalizeTitle(localRoot.agent_id, 'main'), localMainTask?.id || '', localBranch),
  });

  localChildren.forEach((child, idx) => {
    const childX = ORIGIN_X + idx * (CARD_WIDTH + CLUSTER_GAP);
    const stats = taskStats[normalizeTitle(child.agent_id, '')] || emptyStats;
    const task = recentTaskByAgent[normalizeTitle(child.agent_id, '')];
    const childRegistry = registryItems.find((item) => item.agent_id === child.agent_id);
    localBranchStats.running += stats.running;
    localBranchStats.failed += stats.failed;
    cards.push({
      key: `local-child-${child.agent_id || idx}`,
      branch: localBranch,
      agentID: normalizeTitle(child.agent_id, ''),
      transportType: 'local',
      kind: 'agent',
      x: childX,
      y: CHILD_START_Y,
      w: CARD_WIDTH,
      h: CARD_HEIGHT,
      title: normalizeTitle(child.display_name, normalizeTitle(child.agent_id, 'agent')),
      subtitle: `${normalizeTitle(child.agent_id, '-')} · ${normalizeTitle(child.role, '-')}`,
      meta: [
        `total=${stats.total} running=${stats.running}`,
        `waiting=${stats.waiting} failed=${stats.failed}`,
        `notify=${normalizeTitle(childRegistry?.notify_main_policy, 'final_only')}`,
        `transport=${normalizeTitle(child.transport, 'local')} type=${normalizeTitle(child.type, 'worker')}`,
        `tools=${normalizeTitle(childRegistry?.tool_visibility?.mode, 'allowlist')} visible=${childRegistry?.tool_visibility?.effective_tool_count ?? 0} inherited=${childRegistry?.tool_visibility?.inherited_tool_count ?? 0}`,
        (childRegistry?.inherited_tools || []).length ? `inherits: ${(childRegistry?.inherited_tools || []).join(', ')}` : 'inherits: -',
        stats.active[0] ? `task: ${stats.active[0].title}` : task ? `last: ${summarizeTask(task.task, task.label)}` : t('noLiveTasks'),
      ],
      accentTone: stats.running > 0 ? 'success' : stats.latestStatus === 'failed' ? 'danger' : 'info',
      clickable: true,
      scale,
      onClick: () => onOpenAgentStream(normalizeTitle(child.agent_id, ''), task?.id || '', localBranch),
    });
  });

  let remoteOffsetX = ORIGIN_X + localBranchWidth + SECTION_GAP;
  remoteClusters.forEach((cluster, treeIndex) => {
    const { tree, root: treeRoot, children } = cluster;
    if (!treeRoot) {
      remoteOffsetX += cluster.width + SECTION_GAP;
      return;
    }

    const branch = `node:${normalizeTitle(tree.node_id, `remote-${treeIndex}`)}`;
    const nodeID = normalizeTitle(tree.node_id, '');
    const p2pSession = p2pSessionByNode[nodeID];
    const recentDispatch = recentDispatchByNode[nodeID];
    const rootX = remoteOffsetX + Math.max(0, (cluster.width - CARD_WIDTH) / 2);
    const sessionStatus = normalizeTitle(p2pSession?.status, '').toLowerCase();

    cards.push({
      key: `remote-root-${tree.node_id || treeIndex}`,
      branch,
      agentID: normalizeTitle(treeRoot.agent_id, ''),
      transportType: 'remote',
      kind: 'agent',
      x: rootX,
      y: MAIN_Y,
      w: CARD_WIDTH,
      h: CARD_HEIGHT,
      title: normalizeTitle(treeRoot.display_name, treeRoot.agent_id || 'main'),
      subtitle: `${normalizeTitle(treeRoot.agent_id, '-')} · ${normalizeTitle(treeRoot.role, '-')}`,
      meta: [
        `status=${tree.online ? t('online') : t('offline')}`,
        `transport=${normalizeTitle(treeRoot.transport, 'node')} type=${normalizeTitle(treeRoot.type, 'router')}`,
        `p2p=${normalizeTitle(nodeP2PTransport, 'disabled')} session=${normalizeTitle(p2pSession?.status, 'unknown')}`,
        `last_transport=${normalizeTitle(recentDispatch?.used_transport, '-')}${recentDispatch?.fallback_from ? ` fallback=${normalizeTitle(recentDispatch?.fallback_from, '-')}` : ''}`,
        `last_ready=${formatRuntimeTimestamp(p2pSession?.last_ready_at)}`,
        `retry=${Number(p2pSession?.retry_count || 0)}`,
        `${t('error')}=${normalizeTitle(p2pSession?.last_error, '-')}`,
        `source=${normalizeTitle(treeRoot.managed_by, tree.source || '-')}`,
        t('remoteTasksUnavailable'),
      ],
      accentTone: !tree.online ? 'neutral' : sessionStatus === 'open' ? 'success' : sessionStatus === 'connecting' ? 'warning' : 'accent',
      clickable: true,
      scale,
      onClick: () => onOpenAgentStream(normalizeTitle(treeRoot.agent_id, ''), '', branch),
    });

    children.forEach((child, idx) => {
      const childX = remoteOffsetX + idx * (CARD_WIDTH + CLUSTER_GAP);
      cards.push({
        key: `remote-child-${tree.node_id || treeIndex}-${child.agent_id || idx}`,
        branch,
        agentID: normalizeTitle(child.agent_id, ''),
        transportType: 'remote',
        kind: 'agent',
        x: childX,
        y: CHILD_START_Y,
        w: CARD_WIDTH,
        h: CARD_HEIGHT,
        title: normalizeTitle(child.display_name, child.agent_id || 'agent'),
        subtitle: `${normalizeTitle(child.agent_id, '-')} · ${normalizeTitle(child.role, '-')}`,
        meta: [
          `transport=${normalizeTitle(child.transport, 'node')} type=${normalizeTitle(child.type, 'worker')}`,
          `p2p=${normalizeTitle(nodeP2PTransport, 'disabled')} session=${normalizeTitle(p2pSession?.status, 'unknown')}`,
          `last_transport=${normalizeTitle(recentDispatch?.used_transport, '-')}${recentDispatch?.fallback_from ? ` fallback=${normalizeTitle(recentDispatch?.fallback_from, '-')}` : ''}`,
          `last_ready=${formatRuntimeTimestamp(p2pSession?.last_ready_at)}`,
          `retry=${Number(p2pSession?.retry_count || 0)}`,
          `${t('error')}=${normalizeTitle(p2pSession?.last_error, '-')}`,
          `source=${normalizeTitle(child.managed_by, 'remote_webui')}`,
          t('remoteTasksUnavailable'),
        ],
        accentTone: sessionStatus === 'open' ? 'success' : sessionStatus === 'connecting' ? 'warning' : 'accent',
        clickable: true,
        scale,
        onClick: () => onOpenAgentStream(normalizeTitle(child.agent_id, ''), '', branch),
      });
    });

    remoteOffsetX += cluster.width + SECTION_GAP;
  });

  const branchFilters = buildBranchFilters(localBranch, localBranchStats, remoteTrees, topologyFilter);
  const decoratedGraph = decorateGraph(cards, remoteClusters, localChildren, localBranch, selectedTopologyBranch, nodeOverrides, branchFilters);

  return {
    width,
    height,
    cards: decoratedGraph.cards,
    lines: decoratedGraph.lines,
  };
}
