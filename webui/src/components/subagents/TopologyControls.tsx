import React from 'react';

type TopologyFilter = 'all' | 'running' | 'failed' | 'local' | 'remote';

type TopologyControlsProps = {
  onClearFocus: () => void;
  onFitView: () => void;
  onResetZoom: () => void;
  onSelectFilter: (filter: TopologyFilter) => void;
  onZoomIn: () => void;
  onZoomOut: () => void;
  runningCount: number;
  selectedBranch: string;
  t: (key: string, options?: any) => string;
  topologyFilter: TopologyFilter;
  zoomPercent: number;
};

const FILTERS: TopologyFilter[] = ['all', 'running', 'failed', 'local', 'remote'];
const FILTER_LABELS: Record<TopologyFilter, string> = {
  all: 'All',
  running: 'Running',
  failed: 'Failed',
  local: 'Local',
  remote: 'Remote',
};

const TopologyControls: React.FC<TopologyControlsProps> = ({
  onClearFocus,
  onFitView,
  onResetZoom,
  onSelectFilter,
  onZoomIn,
  onZoomOut,
  runningCount,
  selectedBranch,
  t,
  topologyFilter,
  zoomPercent,
}) => {
  return (
    <div className="flex items-center gap-2 flex-wrap justify-end">
      {FILTERS.map((filter) => (
        <button
          key={filter}
          onClick={() => onSelectFilter(filter)}
          className={`px-2 py-1 rounded-xl text-[11px] ${topologyFilter === filter ? 'control-chip-active' : 'control-chip'}`}
        >
          {t(`topologyFilter.${filter}`, { defaultValue: FILTER_LABELS[filter] })}
        </button>
      ))}
      {selectedBranch ? (
        <button onClick={onClearFocus} className="px-2 py-1 rounded-xl text-[11px] control-chip">
          {t('clearFocus')}
        </button>
      ) : null}
      <div className="control-chip-group flex items-center gap-1 rounded-xl px-1 py-1">
        <button onClick={onZoomOut} className="px-2 py-1 rounded-lg text-[11px] control-chip">
          {t('zoomOut')}
        </button>
        <button onClick={onFitView} className="px-2 py-1 rounded-lg text-[11px] control-chip">
          {t('fitView')}
        </button>
        <button onClick={onResetZoom} className="px-2 py-1 rounded-lg text-[11px] control-chip">
          100%
        </button>
        <button onClick={onZoomIn} className="px-2 py-1 rounded-lg text-[11px] control-chip">
          {t('zoomIn')}
        </button>
      </div>
      <div className="text-xs text-zinc-400">
        {zoomPercent}% · {runningCount} {t('runningTasks')}
      </div>
    </div>
  );
};

export default TopologyControls;
