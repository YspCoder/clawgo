import React, { useEffect, useMemo, useState } from 'react';
import { OFFICE_CANVAS, OFFICE_ZONE_POINT, OFFICE_ZONE_SLOTS, OfficeZone } from './officeLayout';

export type OfficeMainState = {
  id?: string;
  name?: string;
  state?: string;
  detail?: string;
  zone?: string;
  task_id?: string;
};

export type OfficeNodeState = {
  id?: string;
  name?: string;
  state?: string;
  zone?: string;
  detail?: string;
  updated_at?: string;
};

type OfficeSceneProps = {
  main: OfficeMainState;
  nodes: OfficeNodeState[];
};

type SpriteSpec = {
  src: string;
  frameW: number;
  frameH: number;
  cols: number;
  start: number;
  end: number;
  fps: number;
  scale: number;
};

const TICK_FPS = 12;

const MAIN_SPRITES: Record<'idle' | 'working' | 'syncing' | 'error', SpriteSpec> = {
  idle: {
    src: '/webui/office/star-idle-v5.png',
    frameW: 256,
    frameH: 256,
    cols: 8,
    start: 0,
    end: 47,
    fps: 12,
    scale: 0.62,
  },
  working: {
    src: '/webui/office/star-working-spritesheet-grid.webp',
    frameW: 300,
    frameH: 300,
    cols: 8,
    start: 0,
    end: 37,
    fps: 12,
    scale: 0.56,
  },
  syncing: {
    src: '/webui/office/sync-animation-v3-grid.webp',
    frameW: 256,
    frameH: 256,
    cols: 7,
    start: 1,
    end: 47,
    fps: 12,
    scale: 0.54,
  },
  error: {
    src: '/webui/office/error-bug-spritesheet-grid.webp',
    frameW: 220,
    frameH: 220,
    cols: 8,
    start: 0,
    end: 71,
    fps: 12,
    scale: 0.66,
  },
};

const NODE_SPRITES: SpriteSpec[] = Array.from({ length: 6 }, (_, i) => ({
  src: `/webui/office/guest_anim_${i + 1}.webp`,
  frameW: 32,
  frameH: 32,
  cols: 8,
  start: 0,
  end: 7,
  fps: 8,
  scale: 1.55,
}));

function normalizeZone(z: string | undefined): OfficeZone {
  const v = (z || '').trim().toLowerCase();
  if (v === 'work' || v === 'server' || v === 'bug' || v === 'breakroom') return v;
  return 'breakroom';
}

function normalizeMainSpriteState(s: string | undefined): keyof typeof MAIN_SPRITES {
  const v = (s || '').trim().toLowerCase();
  if (v.includes('error') || v.includes('blocked')) return 'error';
  if (v.includes('sync') || v.includes('suppressed')) return 'syncing';
  if (v.includes('run') || v.includes('execut') || v.includes('writing') || v.includes('research') || v.includes('success')) return 'working';
  return 'idle';
}

function textHash(input: string): number {
  let h = 0;
  for (let i = 0; i < input.length; i += 1) {
    h = (h * 31 + input.charCodeAt(i)) >>> 0;
  }
  return h;
}

function frameAtTick(spec: SpriteSpec, tick: number, seed = 0): number {
  const frameCount = Math.max(1, spec.end - spec.start + 1);
  const absoluteMs = tick * (1000 / TICK_FPS);
  const frame = Math.floor((absoluteMs + seed) / (1000 / Math.max(1, spec.fps)));
  return spec.start + (frame % frameCount);
}

type SpriteProps = {
  spec: SpriteSpec;
  frame: number;
  className?: string;
};

const SpriteSheet: React.FC<SpriteProps> = ({ spec, frame, className }) => {
  const col = frame % spec.cols;
  const row = Math.floor(frame / spec.cols);
  return (
    <div
      className={className}
      style={{
        width: spec.frameW,
        height: spec.frameH,
        backgroundImage: `url(${spec.src})`,
        backgroundRepeat: 'no-repeat',
        backgroundPosition: `-${col * spec.frameW}px -${row * spec.frameH}px`,
        imageRendering: 'pixelated',
        transform: `translate(-50%, -50%) scale(${spec.scale})`,
        transformOrigin: 'center center',
      }}
    />
  );
};

const OfficeScene: React.FC<OfficeSceneProps> = ({ main, nodes }) => {
  const [tick, setTick] = useState(0);

  useEffect(() => {
    const timer = window.setInterval(() => {
      setTick((v) => (v + 1) % 10000000);
    }, Math.round(1000 / TICK_FPS));
    return () => window.clearInterval(timer);
  }, []);

  const bgSrc = '/webui/office/office_bg_small.webp';
  const placedNodes = useMemo(() => {
    const counters: Record<OfficeZone, number> = { breakroom: 0, work: 0, server: 0, bug: 0 };
    return nodes.slice(0, 24).map((n) => {
      const zone = normalizeZone(n.zone);
      const slots = OFFICE_ZONE_SLOTS[zone];
      const idx = counters[zone] % slots.length;
      counters[zone] += 1;
      const stableKey = `${n.id || ''}|${n.name || ''}|${idx}`;
      const avatarSeed = textHash(stableKey);
      const spriteIndex = avatarSeed % NODE_SPRITES.length;
      return { ...n, zone, point: slots[idx], spriteIndex, avatarSeed };
    });
  }, [nodes]);

  const mainZone = normalizeZone(main.zone);
  const mainPoint = OFFICE_ZONE_POINT[mainZone];
  const mainSprite = MAIN_SPRITES[normalizeMainSpriteState(main.state)];
  const mainFrame = frameAtTick(mainSprite, tick);

  return (
    <div className="relative w-full overflow-hidden rounded-2xl border border-zinc-800 bg-zinc-950/60">
      <div className="relative aspect-[16/9]">
        <img src={bgSrc} alt="office" className="absolute inset-0 h-full w-full object-cover" />
        <div className="absolute inset-0">
          <div
            className="absolute -translate-x-1/2 -translate-y-1/2"
            style={{
              left: `${(mainPoint.x / OFFICE_CANVAS.width) * 100}%`,
              top: `${(mainPoint.y / OFFICE_CANVAS.height) * 100}%`,
            }}
            title={`${main.state || 'idle'} ${main.detail || ''}`.trim()}
          >
            <div className="relative">
              <SpriteSheet spec={mainSprite} frame={mainFrame} className="absolute left-1/2 top-1/2" />
              <div className="absolute left-1/2 top-1/2 -translate-x-1/2 translate-y-[62px] rounded bg-black/75 px-2 py-0.5 text-[10px] font-semibold tracking-wide text-zinc-100">
                {main.name || main.id || 'main'}
              </div>
            </div>
          </div>

          {placedNodes.map((n, i) => (
            <div
              key={`${n.id || 'node'}-${i}`}
              className="absolute -translate-x-1/2 -translate-y-1/2"
              style={{
                left: `${(n.point.x / OFFICE_CANVAS.width) * 100}%`,
                top: `${(n.point.y / OFFICE_CANVAS.height) * 100}%`,
              }}
              title={`${n.name || n.id || 'node'} · ${n.state || 'idle'}${n.detail ? ` · ${n.detail}` : ''}`}
            >
              <div className="relative">
                <SpriteSheet
                  spec={NODE_SPRITES[n.spriteIndex]}
                  frame={frameAtTick(NODE_SPRITES[n.spriteIndex], tick, n.avatarSeed % 1000)}
                  className="absolute left-1/2 top-1/2"
                />
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
};

export default OfficeScene;
