import React, { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import { RefreshCw } from 'lucide-react';
import { FixedButton } from '../components/ui/Button';
import PageHeader from '../components/layout/PageHeader';
import SectionPanel from '../components/layout/SectionPanel';
import GraphCard from '../components/subagents/GraphCard';
import TopologyCanvas from '../components/subagents/TopologyCanvas';
import TopologyControls from '../components/subagents/TopologyControls';
import TopologyTooltip from '../components/subagents/TopologyTooltip';
import type { GraphCardSpec } from '../components/subagents/topologyTypes';
import { buildTopologyGraph } from '../components/subagents/topologyGraphBuilder';
import { useTopologyViewport } from '../components/subagents/useTopologyViewport';
import { useSubagentRuntimeData } from '../components/subagents/useSubagentRuntimeData';
import type { NodeTree } from '../components/subagents/subagentTypes';
import {
  formatStreamTime,
  getTopologyTooltipPosition,
  normalizeTitle,
  summarizePreviewText,
} from '../components/subagents/topologyUtils';

type TopologyTooltipState = {
  title: string;
  subtitle: string;
  meta: string[];
  x: number;
  y: number;
  agentID?: string;
  transportType?: 'local' | 'remote';
} | null;

const Subagents: React.FC = () => {
  const { t } = useTranslation();
  const { q, nodeTrees, nodeP2P, nodeDispatchItems, subagentRuntimeItems, subagentRegistryItems } = useAppContext();
  const [selectedTopologyBranch, setSelectedTopologyBranch] = useState('');
  const [topologyFilter, setTopologyFilter] = useState<'all' | 'running' | 'failed' | 'local' | 'remote'>('all');
  const [topologyTooltip, setTopologyTooltip] = useState<TopologyTooltipState>(null);
  const hasFittedRef = useRef(false);
  const previewAgentID = topologyTooltip?.transportType === 'local' ? String(topologyTooltip.agentID || '').trim() : '';
  const {
    items,
    recentTaskByAgent,
    refresh,
    registryItems,
    setSelectedAgentID,
    setSelectedId,
    streamPreviewByAgent,
    taskStats,
  } = useSubagentRuntimeData({
    previewAgentID,
    q,
    subagentRegistryItems,
    subagentRuntimeItems,
  });

  const openAgentStream = (agentID: string, taskID = '', branch = '') => {
    if (branch) setSelectedTopologyBranch(branch);
    setSelectedAgentID(agentID);
    setSelectedId(taskID);
  };

  const parsedNodeTrees = useMemo<NodeTree[]>(() => {
    try {
      const parsed = JSON.parse(nodeTrees);
      return Array.isArray(parsed) ? parsed : [];
    } catch {
      return [];
    }
  }, [nodeTrees]);
  const p2pSessionByNode = useMemo(() => {
    const out: Record<string, any> = {};
    const sessions = Array.isArray(nodeP2P?.nodes) ? nodeP2P.nodes : [];
    sessions.forEach((session: any) => {
      const nodeID = normalizeTitle(session?.node, '');
      if (!nodeID) return;
      out[nodeID] = session;
    });
    return out;
  }, [nodeP2P]);
  const recentDispatchByNode = useMemo(() => {
    const out: Record<string, any> = {};
    const rows = Array.isArray(nodeDispatchItems) ? nodeDispatchItems : [];
    rows.forEach((row: any) => {
      const nodeID = normalizeTitle(row?.node, '');
      if (!nodeID || out[nodeID]) return;
      out[nodeID] = row;
    });
    return out;
  }, [nodeDispatchItems]);
  const clearTopologyTooltip = () => setTopologyTooltip(null);
  const {
    draggedNode,
    handleNodeDragStart,
    handleTopologyResetZoom,
    handleTopologyZoomIn,
    handleTopologyZoomOut,
    fitView,
    moveTopologyDrag,
    nodeOverrides,
    startTopologyDrag,
    stopTopologyDrag,
    topologyDragging,
    topologyPan,
    topologyViewportRef,
    topologyZoom,
  } = useTopologyViewport({
    clearTooltip: clearTopologyTooltip,
  });
  const topologyGraph = useMemo(() => {
    return buildTopologyGraph({
      parsedNodeTrees,
      registryItems,
      taskStats,
      recentTaskByAgent,
      selectedTopologyBranch,
      topologyFilter,
      topologyZoom,
      nodeOverrides,
      nodeP2PTransport: nodeP2P?.transport,
      p2pSessionByNode,
      recentDispatchByNode,
      t,
      onOpenAgentStream: openAgentStream,
    });
  }, [parsedNodeTrees, registryItems, taskStats, recentTaskByAgent, selectedTopologyBranch, topologyFilter, t, topologyZoom, nodeOverrides, nodeP2P, p2pSessionByNode, recentDispatchByNode]);

  useEffect(() => {
    if (!hasFittedRef.current && topologyGraph.width > 0) {
      fitView(topologyGraph.width, topologyGraph.height);
      hasFittedRef.current = true;
    }
  }, [fitView, topologyGraph.height, topologyGraph.width]);

  const handleTopologyHover = (card: GraphCardSpec, event: React.MouseEvent<HTMLDivElement>) => {
    const { x, y } = getTopologyTooltipPosition(event.clientX, event.clientY);

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

  return (
    <div className="h-full p-4 md:p-6 xl:p-8 flex flex-col gap-4">
      <PageHeader
        title={t('subagentsRuntime')}
        actions={
          <FixedButton onClick={() => refresh()} variant="primary" label={t('refresh')}>
            <RefreshCw className="w-4 h-4" />
          </FixedButton>
        }
      />

      <SectionPanel
        title={t('agentTopology')}
        subtitle={t('agentTopologyHint')}
        className="flex-1 min-h-0 p-4 flex flex-col gap-3"
        headerClassName="mb-0"
        actions={
          <TopologyControls
            onClearFocus={() => setSelectedTopologyBranch('')}
            onFitView={() => fitView(topologyGraph.width, topologyGraph.height)}
            onResetZoom={handleTopologyResetZoom}
            onSelectFilter={setTopologyFilter}
            onZoomIn={handleTopologyZoomIn}
            onZoomOut={handleTopologyZoomOut}
            runningCount={items.filter((item) => item.status === 'running').length}
            selectedBranch={selectedTopologyBranch}
            t={t}
            topologyFilter={topologyFilter}
            zoomPercent={Math.round(topologyZoom * 100)}
          />
        }
      >
        <TopologyCanvas
          cards={topologyGraph.cards.map((card) => (
            card.hidden ? null : (
              <GraphCard
                key={card.key}
                card={card}
                onHover={handleTopologyHover}
                onLeave={clearTopologyTooltip}
                onDragStart={(key, event) => handleNodeDragStart(key, event, (cardKey) => {
                  const target = topologyGraph.cards.find((item) => item.key === cardKey);
                  return target ? { key: target.key, x: target.x, y: target.y } : null;
                })}
              />
            )
          ))}
          draggedNode={!!draggedNode}
          height={topologyGraph.height}
          lines={topologyGraph.lines}
          onMouseDown={startTopologyDrag}
          onMouseLeave={() => {
            stopTopologyDrag();
            clearTopologyTooltip();
          }}
          onMouseMove={moveTopologyDrag}
          onMouseUp={stopTopologyDrag}
          panX={topologyPan.x}
          panY={topologyPan.y}
          topologyDragging={topologyDragging}
          tooltip={topologyTooltip ? (
            <TopologyTooltip
              agentID={topologyTooltip.agentID}
              formatStreamTime={formatStreamTime}
              meta={topologyTooltip.meta}
              streamPreview={topologyTooltip.agentID ? streamPreviewByAgent[topologyTooltip.agentID] : undefined}
              subtitle={topologyTooltip.subtitle}
              summarizePreviewText={summarizePreviewText}
              t={t}
              title={topologyTooltip.title}
              transportType={topologyTooltip.transportType}
              x={topologyTooltip.x}
              y={topologyTooltip.y}
            />
          ) : null}
          viewportRef={topologyViewportRef}
          width={topologyGraph.width}
          zoom={topologyZoom}
        />
      </SectionPanel>

    </div>
  );
};

export default Subagents;
