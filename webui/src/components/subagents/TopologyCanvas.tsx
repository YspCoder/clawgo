import React from 'react';
import { SpaceParticles } from '../SpaceParticles';
import type { GraphLineSpec } from './topologyTypes';

type TopologyCanvasProps = {
  cards: React.ReactNode;
  className?: string;
  draggedNode: boolean;
  height: number;
  lines: GraphLineSpec[];
  onMouseDown: React.MouseEventHandler<HTMLDivElement>;
  onMouseLeave: React.MouseEventHandler<HTMLDivElement>;
  onMouseMove: React.MouseEventHandler<HTMLDivElement>;
  onMouseUp: React.MouseEventHandler<HTMLDivElement>;
  panX: number;
  panY: number;
  topologyDragging: boolean;
  tooltip?: React.ReactNode;
  viewportRef: React.RefObject<HTMLDivElement | null>;
  width: number;
  zoom: number;
};

const FLOW_STYLE = `
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
`;

const TopologyCanvas: React.FC<TopologyCanvasProps> = ({
  cards,
  className,
  draggedNode,
  height,
  lines,
  onMouseDown,
  onMouseLeave,
  onMouseMove,
  onMouseUp,
  panX,
  panY,
  topologyDragging,
  tooltip,
  viewportRef,
  width,
  zoom,
}) => {
  return (
    <div
      ref={viewportRef}
      onMouseDown={onMouseDown}
      onMouseMove={onMouseMove}
      onMouseUp={onMouseUp}
      onMouseLeave={onMouseLeave}
      className={className || 'radius-canvas relative flex-1 min-h-[420px] sm:min-h-[560px] xl:min-h-[760px] overflow-hidden border border-zinc-800 bg-zinc-950/80'}
      style={{ cursor: topologyDragging ? 'grabbing' : 'grab' }}
    >
      <SpaceParticles />
      <div className="absolute inset-0 z-10">
        <div
          style={{
            transform: `translate(${panX}px, ${panY}px) scale(${zoom})`,
            transformOrigin: '0 0',
            width,
            height,
            transition: topologyDragging || draggedNode ? 'none' : 'transform 0.1s ease-out',
          }}
          className="relative will-change-transform"
        >
          <svg
            width={width}
            height={height}
            className="block absolute top-0 left-0 overflow-visible"
          >
            <style>{FLOW_STYLE}</style>
            {lines.map((line, idx) => (
              line.hidden ? null : (
                <g key={`line-${idx}`}>
                  <path
                    d={line.path}
                    fill="none"
                    stroke={line.highlighted ? 'var(--topology-line-highlight-track)' : 'var(--topology-line-track)'}
                    strokeWidth={line.highlighted ? '6' : '2'}
                    strokeLinecap="round"
                    className="transition-all duration-300"
                  />
                  <path
                    d={line.path}
                    fill="none"
                    stroke={line.highlighted ? 'var(--topology-line-highlight-flow)' : 'var(--topology-line-flow)'}
                    strokeWidth={line.highlighted ? '2.5' : '1.5'}
                    strokeDasharray={line.highlighted ? '6 18' : '4 20'}
                    className={line.highlighted ? 'animate-flow-fast' : 'animate-flow'}
                    strokeLinecap="round"
                    opacity={line.dimmed ? 0.1 : 1}
                  />
                </g>
              )
            ))}
            {cards}
          </svg>
        </div>
      </div>
      {tooltip}
    </div>
  );
};

export default TopologyCanvas;
