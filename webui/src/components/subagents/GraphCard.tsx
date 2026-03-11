import React from 'react';
import { Cpu, Server } from 'lucide-react';
import type { GraphAccentTone, GraphCardSpec } from './topologyTypes';

function graphAccentBackgroundClass(accentTone: GraphAccentTone) {
  switch (accentTone) {
    case 'success': return 'bg-gradient-to-br from-transparent to-emerald-500';
    case 'danger': return 'bg-gradient-to-br from-transparent to-red-500';
    case 'warning': return 'bg-gradient-to-br from-transparent to-amber-400';
    case 'info': return 'bg-gradient-to-br from-transparent to-sky-400';
    case 'accent': return 'bg-gradient-to-br from-transparent to-violet-400';
    case 'neutral':
    default: return 'bg-gradient-to-br from-transparent to-zinc-500';
  }
}

function graphAccentIconClass(accentTone: GraphAccentTone) {
  switch (accentTone) {
    case 'success': return 'text-emerald-500';
    case 'danger': return 'topology-icon-danger';
    case 'warning': return 'text-amber-400';
    case 'info': return 'text-sky-400';
    case 'accent': return 'text-violet-400';
    case 'neutral':
    default: return 'text-zinc-500';
  }
}

type GraphCardProps = {
  card: GraphCardSpec;
  onDragStart: (key: string, event: React.MouseEvent<HTMLDivElement>) => void;
  onHover: (card: GraphCardSpec, event: React.MouseEvent<HTMLDivElement>) => void;
  onLeave: () => void;
};

const GraphCard: React.FC<GraphCardProps> = ({
  card,
  onDragStart,
  onHover,
  onLeave,
}) => {
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
        className={`relative w-full h-full rounded-full flex flex-col items-center justify-center gap-1 transition-all duration-300 group ${card.highlighted ? 'scale-[1.05] z-10' : 'hover:scale-[1.02]'}`}
        style={{
          cursor: card.clickable ? 'pointer' : 'default',
          opacity: card.dimmed ? 0.3 : 1,
        }}
      >
        <div className={`absolute inset-0 rounded-full transition-all duration-300 backdrop-blur-md ${card.highlighted ? 'topology-node-highlight' : 'topology-node-base'}`}>
          <div className="absolute inset-0 rounded-full bg-gradient-to-b from-zinc-800/95 to-zinc-950/95" />
          <div className={`absolute inset-0 rounded-full opacity-20 ${graphAccentBackgroundClass(card.accentTone)}`} />
          <div className="topology-node-inner-border absolute inset-[1px] rounded-full border" />
          <div className={`absolute inset-0 rounded-full border-[1.5px] ${card.highlighted ? 'topology-node-border-highlight' : 'border-zinc-700/80 group-hover:border-zinc-500/80'}`} />
        </div>

        <div className="relative z-10 flex flex-col items-center justify-center w-full px-4 text-center">
          <div className="flex items-center justify-center w-10 h-10 mb-1 rounded-full bg-zinc-950/60 border border-zinc-700/50 shadow-inner backdrop-blur-sm">
            <Icon className={`w-5 h-5 ${graphAccentIconClass(card.accentTone)}`} />
          </div>

          <div className="w-full">
            <div className="text-[13px] font-bold text-zinc-100 truncate leading-tight drop-shadow-md">{card.title}</div>
            <div className="text-[10px] text-zinc-300/90 truncate mt-0.5 drop-shadow-sm">{card.subtitle}</div>
          </div>

          {card.online !== undefined ? (
            <div className={`absolute top-6 right-6 w-2.5 h-2.5 rounded-full border border-zinc-900 ${card.online ? 'status-dot-online topology-online-indicator' : 'status-dot-offline'}`} />
          ) : null}
        </div>
      </div>
    </foreignObject>
  );
};

export default GraphCard;
