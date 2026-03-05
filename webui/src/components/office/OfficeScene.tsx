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

type ImageSpec = {
  src: string;
  width: number;
  height: number;
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

const DECOR_SPRITES = {
  plants: {
    src: '/webui/office/plants-spritesheet.webp',
    frameW: 160,
    frameH: 160,
    cols: 4,
    start: 0,
    end: 15,
    fps: 0,
    scale: 1,
  } as SpriteSpec,
  posters: {
    src: '/webui/office/posters-spritesheet.webp',
    frameW: 160,
    frameH: 160,
    cols: 4,
    start: 0,
    end: 31,
    fps: 0,
    scale: 1,
  } as SpriteSpec,
  flowers: {
    src: '/webui/office/flowers-bloom-v2.webp',
    frameW: 128,
    frameH: 128,
    cols: 4,
    start: 0,
    end: 15,
    fps: 0,
    scale: 0.8,
  } as SpriteSpec,
  cats: {
    src: '/webui/office/cats-spritesheet.webp',
    frameW: 160,
    frameH: 160,
    cols: 4,
    start: 0,
    end: 15,
    fps: 0,
    scale: 1,
  } as SpriteSpec,
  coffeeMachine: {
    src: '/webui/office/coffee-machine-v3-grid.webp',
    frameW: 230,
    frameH: 230,
    cols: 12,
    start: 0,
    end: 94,
    fps: 10,
    scale: 1,
  } as SpriteSpec,
  serverroom: {
    src: '/webui/office/serverroom-spritesheet.webp',
    frameW: 180,
    frameH: 251,
    cols: 40,
    start: 0,
    end: 38,
    fps: 6,
    scale: 1,
  } as SpriteSpec,
};

const DECOR_IMAGES = {
  sofaIdle: {
    src: '/webui/office/sofa-idle-v3.png',
    width: 212,
    height: 143,
    scale: 1,
  } as ImageSpec,
  sofaShadow: {
    src: '/webui/office/sofa-shadow-v1.png',
    width: 233,
    height: 81,
    scale: 1,
  } as ImageSpec,
  desk: {
    src: '/webui/office/desk-v3.webp',
    width: 304,
    height: 264,
    scale: 1,
  } as ImageSpec,
  coffeeShadow: {
    src: '/webui/office/coffee-machine-shadow-v1.png',
    width: 245,
    height: 111,
    scale: 1,
  } as ImageSpec,
};

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

function frameFromSeed(spec: SpriteSpec, seedText: string): number {
  const frameCount = Math.max(1, spec.end - spec.start + 1);
  return spec.start + (textHash(seedText) % frameCount);
}

function posStyle(x: number, y: number, zIndex: number): React.CSSProperties {
  return {
    left: `${(x / OFFICE_CANVAS.width) * 100}%`,
    top: `${(y / OFFICE_CANVAS.height) * 100}%`,
    zIndex,
  };
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

type PlacedSpriteProps = {
  spec: SpriteSpec;
  frame: number;
  x: number;
  y: number;
  zIndex: number;
  title?: string;
};

const PlacedSprite: React.FC<PlacedSpriteProps> = ({ spec, frame, x, y, zIndex, title }) => (
  <div className="absolute -translate-x-1/2 -translate-y-1/2 pointer-events-none" style={posStyle(x, y, zIndex)} title={title || ''}>
    <div className="relative">
      <SpriteSheet spec={spec} frame={frame} className="absolute left-1/2 top-1/2" />
    </div>
  </div>
);

type PlacedImageProps = {
  spec: ImageSpec;
  x: number;
  y: number;
  zIndex: number;
  title?: string;
};

const PlacedImage: React.FC<PlacedImageProps> = ({ spec, x, y, zIndex, title }) => (
  <div className="absolute -translate-x-1/2 -translate-y-1/2 pointer-events-none" style={posStyle(x, y, zIndex)} title={title || ''}>
    <img
      src={spec.src}
      alt=""
      className="absolute left-1/2 top-1/2"
      style={{
        width: spec.width,
        height: spec.height,
        imageRendering: 'pixelated',
        transform: `translate(-50%, -50%) scale(${spec.scale})`,
        transformOrigin: 'center center',
      }}
    />
  </div>
);

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
  const mainSpriteState = normalizeMainSpriteState(main.state);

  const decorSeedBase = `${main.id || 'main'}|${main.name || ''}`;
  const plantFrameA = frameFromSeed(DECOR_SPRITES.plants, `${decorSeedBase}|plantA`);
  const plantFrameB = frameFromSeed(DECOR_SPRITES.plants, `${decorSeedBase}|plantB`);
  const plantFrameC = frameFromSeed(DECOR_SPRITES.plants, `${decorSeedBase}|plantC`);
  const posterFrame = frameFromSeed(DECOR_SPRITES.posters, `${decorSeedBase}|poster`);
  const flowerFrame = frameFromSeed(DECOR_SPRITES.flowers, `${decorSeedBase}|flower`);
  const catFrame = frameFromSeed(DECOR_SPRITES.cats, `${decorSeedBase}|cat`);
  const coffeeFrame = frameAtTick(DECOR_SPRITES.coffeeMachine, tick, 300);
  const serverFrame = mainSpriteState === 'idle' ? 0 : frameAtTick(DECOR_SPRITES.serverroom, tick, 700);

  return (
    <div className="relative w-full overflow-hidden rounded-2xl border border-zinc-800 bg-zinc-950/60">
      <div className="relative aspect-[16/9]">
        <img src={bgSrc} alt="office" className="absolute inset-0 h-full w-full object-cover" />
        <div className="absolute inset-0">
          <PlacedSprite spec={DECOR_SPRITES.serverroom} frame={serverFrame} x={1021} y={142} zIndex={10} title="serverroom" />

          <PlacedSprite spec={DECOR_SPRITES.posters} frame={posterFrame} x={252} y={66} zIndex={20} title="poster" />
          <PlacedSprite spec={DECOR_SPRITES.plants} frame={plantFrameA} x={565} y={178} zIndex={21} title="plant" />
          <PlacedSprite spec={DECOR_SPRITES.plants} frame={plantFrameB} x={230} y={185} zIndex={21} title="plant" />
          <PlacedSprite spec={DECOR_SPRITES.plants} frame={plantFrameC} x={977} y={496} zIndex={21} title="plant" />

          <PlacedImage spec={DECOR_IMAGES.sofaShadow} x={1070} y={610} zIndex={25} title="sofa-shadow" />
          <PlacedImage spec={DECOR_IMAGES.sofaIdle} x={1070} y={610} zIndex={26} title="sofa" />

          <PlacedImage spec={DECOR_IMAGES.coffeeShadow} x={659} y={397} zIndex={30} title="coffee-shadow" />
          <PlacedSprite spec={DECOR_SPRITES.coffeeMachine} frame={coffeeFrame} x={659} y={397} zIndex={31} title="coffee-machine" />

          <PlacedImage spec={DECOR_IMAGES.desk} x={218} y={417} zIndex={35} title="desk" />
          <PlacedSprite spec={DECOR_SPRITES.flowers} frame={flowerFrame} x={310} y={390} zIndex={36} title="flower" />

          <div
            className="absolute -translate-x-1/2 -translate-y-1/2"
            style={{
              left: `${(mainPoint.x / OFFICE_CANVAS.width) * 100}%`,
              top: `${(mainPoint.y / OFFICE_CANVAS.height) * 100}%`,
              zIndex: 50,
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
                zIndex: 51,
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

          <PlacedSprite spec={DECOR_SPRITES.cats} frame={catFrame} x={94} y={557} zIndex={80} title="cat" />
        </div>
      </div>
    </div>
  );
};

export default OfficeScene;
