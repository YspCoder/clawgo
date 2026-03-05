import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
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

type Point = { x: number; y: number };
type CameraState = { x: number; y: number; zoom: number };
type MainVisualState = 'idle' | 'working' | 'syncing' | 'error';

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

type BubbleState = {
  text: string;
  expiresAt: number;
};

type DecorFrames = {
  plantA: number;
  plantB: number;
  plantC: number;
  poster: number;
  flower: number;
  cat: number;
};

const TICK_FPS = 12;
const RENDER_INTERVAL_MS = 33;
const MIN_ZOOM = 1;
const MAX_ZOOM = 2.4;

const MAIN_SPRITES: Record<MainVisualState, SpriteSpec> = {
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
  bg: {
    src: '/webui/office/office_bg.webp',
    width: OFFICE_CANVAS.width,
    height: OFFICE_CANVAS.height,
    scale: 1,
  } as ImageSpec,
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

const MAIN_BUBBLE_TEXTS: Record<MainVisualState, string[]> = {
  idle: ['Standby mode.', 'Waiting for the next task.', 'System is stable.'],
  working: ['Working on it.', 'Executing workflow.', 'Processing tasks now.'],
  syncing: ['Sync in progress.', 'Collecting updates.', 'Linking status streams.'],
  error: ['Error detected.', 'Need intervention.', 'Investigating abnormal state.'],
};

const NODE_BUBBLE_TEXTS: Record<MainVisualState | 'offline', string[]> = {
  idle: ['Idle.', 'Ready.', 'No active job.'],
  working: ['Working.', 'Running.', 'Task in progress.'],
  syncing: ['Syncing.', 'Updating status.', 'Heartbeat active.'],
  error: ['Failure.', 'Blocked.', 'Error surfaced.'],
  offline: ['Disconnected.', 'No heartbeat.', 'Offline.'],
};

function normalizeZone(z: string | undefined): OfficeZone {
  const v = (z || '').trim().toLowerCase();
  if (v === 'work' || v === 'server' || v === 'bug' || v === 'breakroom') return v;
  return 'breakroom';
}

function normalizeMainState(s: string | undefined): MainVisualState {
  const v = (s || '').trim().toLowerCase();
  if (v.includes('error') || v.includes('blocked')) return 'error';
  if (v.includes('sync') || v.includes('suppressed')) return 'syncing';
  if (v.includes('run') || v.includes('execut') || v.includes('writing') || v.includes('research') || v.includes('success')) return 'working';
  return 'idle';
}

function normalizeNodeState(s: string | undefined): MainVisualState | 'offline' {
  const v = (s || '').trim().toLowerCase();
  if (v.includes('offline')) return 'offline';
  if (v.includes('error') || v.includes('blocked')) return 'error';
  if (v.includes('sync') || v.includes('suppressed')) return 'syncing';
  if (v.includes('run') || v.includes('execut') || v.includes('writing') || v.includes('research') || v.includes('success') || v.includes('online')) {
    return 'working';
  }
  return 'idle';
}

function zoneForMainState(state: MainVisualState): OfficeZone {
  switch (state) {
    case 'working':
      return 'work';
    case 'syncing':
      return 'server';
    case 'error':
      return 'bug';
    default:
      return 'breakroom';
  }
}

function textHash(input: string): number {
  let h = 0;
  for (let i = 0; i < input.length; i += 1) h = (h * 31 + input.charCodeAt(i)) >>> 0;
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

function randFrame(spec: SpriteSpec): number {
  const frameCount = Math.max(1, spec.end - spec.start + 1);
  return spec.start + Math.floor(Math.random() * frameCount);
}

function lerpPoint(current: Point, target: Point, alpha: number): Point {
  const x = current.x + (target.x - current.x) * alpha;
  const y = current.y + (target.y - current.y) * alpha;
  if (Math.abs(target.x - x) < 0.3 && Math.abs(target.y - y) < 0.3) return target;
  return { x, y };
}

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value));
}

function clampCamera(cam: CameraState): CameraState {
  const zoom = clamp(cam.zoom, MIN_ZOOM, MAX_ZOOM);
  const viewW = OFFICE_CANVAS.width / zoom;
  const viewH = OFFICE_CANVAS.height / zoom;
  const maxX = Math.max(0, OFFICE_CANVAS.width - viewW);
  const maxY = Math.max(0, OFFICE_CANVAS.height - viewH);
  return {
    zoom,
    x: clamp(cam.x, 0, maxX),
    y: clamp(cam.y, 0, maxY),
  };
}

function worldToScreen(point: Point, camera: CameraState): Point {
  const viewW = OFFICE_CANVAS.width / camera.zoom;
  const viewH = OFFICE_CANVAS.height / camera.zoom;
  return {
    x: ((point.x - camera.x) / viewW) * 100,
    y: ((point.y - camera.y) / viewH) * 100,
  };
}

function pickRandom<T>(arr: T[]): T | null {
  if (!arr.length) return null;
  return arr[Math.floor(Math.random() * arr.length)] || null;
}

type SpriteProps = {
  spec: SpriteSpec;
  frame: number;
  scaleMultiplier?: number;
  className?: string;
};

const SpriteSheet: React.FC<SpriteProps> = ({ spec, frame, scaleMultiplier = 1, className }) => {
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
        transform: `translate(-50%, -50%) scale(${spec.scale * scaleMultiplier})`,
        transformOrigin: 'center center',
      }}
    />
  );
};

type PlacedSpriteProps = {
  spec: SpriteSpec;
  frame: number;
  point: Point;
  camera: CameraState;
  zIndex: number;
  title?: string;
  scaleMultiplier?: number;
  onClick?: () => void;
};

const PlacedSprite: React.FC<PlacedSpriteProps> = ({ spec, frame, point, camera, zIndex, title, scaleMultiplier = 1, onClick }) => {
  const pos = worldToScreen(point, camera);
  return (
    <div
      className="absolute -translate-x-1/2 -translate-y-1/2"
      style={{ left: `${pos.x}%`, top: `${pos.y}%`, zIndex, pointerEvents: onClick ? 'auto' : 'none' }}
      title={title || ''}
      onClick={onClick}
    >
      <div className="relative">
        <SpriteSheet spec={spec} frame={frame} scaleMultiplier={scaleMultiplier} className="absolute left-1/2 top-1/2" />
      </div>
    </div>
  );
};

type PlacedImageProps = {
  spec: ImageSpec;
  point: Point;
  camera: CameraState;
  zIndex: number;
  title?: string;
  scaleMultiplier?: number;
  onClick?: () => void;
};

const PlacedImage: React.FC<PlacedImageProps> = ({ spec, point, camera, zIndex, title, scaleMultiplier = 1, onClick }) => {
  const pos = worldToScreen(point, camera);
  return (
    <div
      className="absolute -translate-x-1/2 -translate-y-1/2"
      style={{ left: `${pos.x}%`, top: `${pos.y}%`, zIndex, pointerEvents: onClick ? 'auto' : 'none' }}
      title={title || ''}
      onClick={onClick}
    >
      <img
        src={spec.src}
        alt=""
        className="absolute left-1/2 top-1/2"
        style={{
          width: spec.width,
          height: spec.height,
          imageRendering: 'pixelated',
          transform: `translate(-50%, -50%) scale(${spec.scale * scaleMultiplier})`,
          transformOrigin: 'center center',
        }}
      />
    </div>
  );
};

type BubbleProps = {
  point: Point;
  camera: CameraState;
  text: string;
  zIndex: number;
};

const Bubble: React.FC<BubbleProps> = ({ point, camera, text, zIndex }) => {
  const p = worldToScreen({ x: point.x, y: point.y - 70 }, camera);
  return (
    <div
      className="absolute -translate-x-1/2 -translate-y-1/2 pointer-events-none"
      style={{ left: `${p.x}%`, top: `${p.y}%`, zIndex }}
    >
      <div className="rounded-md border border-zinc-700 bg-white/90 px-2 py-1 text-[11px] font-medium text-zinc-900 shadow">
        {text}
      </div>
    </div>
  );
};

const OfficeScene: React.FC<OfficeSceneProps> = ({ main, nodes }) => {
  const viewportRef = useRef<HTMLDivElement | null>(null);
  const cameraRef = useRef<CameraState>({ x: 0, y: 0, zoom: 1 });
  const panRef = useRef<{ active: boolean; x: number; y: number } | null>(null);
  const nextBubbleAtRef = useRef<number>(Date.now() + 2400);

  const [tick, setTick] = useState(0);
  const [showDebug, setShowDebug] = useState(false);
  const [manualState, setManualState] = useState<MainVisualState | null>(null);
  const [panEnabled, setPanEnabled] = useState<boolean>(() => true);
  const [camera, setCamera] = useState<CameraState>({ x: 0, y: 0, zoom: 1 });
  const [pointerWorld, setPointerWorld] = useState<Point | null>(null);

  const liveMainState = normalizeMainState(main.state);
  const effectiveMainState = manualState ?? liveMainState;
  const mainZone = zoneForMainState(effectiveMainState);
  const mainTarget = OFFICE_ZONE_POINT[mainZone];
  const prevLiveStateRef = useRef<MainVisualState>(liveMainState);

  useEffect(() => {
    if (manualState && prevLiveStateRef.current !== liveMainState) {
      setManualState(null);
    }
    prevLiveStateRef.current = liveMainState;
  }, [liveMainState, manualState]);

  const placedNodes = useMemo(() => {
    const counters: Record<OfficeZone, number> = { breakroom: 0, work: 0, server: 0, bug: 0 };
    return nodes.slice(0, 24).map((n, i) => {
      const zone = normalizeZone(n.zone);
      const slots = OFFICE_ZONE_SLOTS[zone];
      const idx = counters[zone] % slots.length;
      counters[zone] += 1;
      const key = `${n.id || 'node'}-${i}`;
      const avatarSeed = textHash(`${n.id || ''}|${n.name || ''}|${idx}`);
      return {
        ...n,
        key,
        zone,
        point: slots[idx],
        spriteIndex: avatarSeed % NODE_SPRITES.length,
        avatarSeed,
      };
    });
  }, [nodes]);

  const [mainPos, setMainPos] = useState<Point>(mainTarget);
  const [nodePos, setNodePos] = useState<Record<string, Point>>({});
  const [mainBubble, setMainBubble] = useState<BubbleState | null>(null);
  const [nodeBubbles, setNodeBubbles] = useState<Record<string, BubbleState>>({});

  const decorSeedBase = `${main.id || 'main'}|${main.name || ''}`;
  const seededDecorFrames = useMemo<DecorFrames>(() => ({
    plantA: frameFromSeed(DECOR_SPRITES.plants, `${decorSeedBase}|plantA`),
    plantB: frameFromSeed(DECOR_SPRITES.plants, `${decorSeedBase}|plantB`),
    plantC: frameFromSeed(DECOR_SPRITES.plants, `${decorSeedBase}|plantC`),
    poster: frameFromSeed(DECOR_SPRITES.posters, `${decorSeedBase}|poster`),
    flower: frameFromSeed(DECOR_SPRITES.flowers, `${decorSeedBase}|flower`),
    cat: frameFromSeed(DECOR_SPRITES.cats, `${decorSeedBase}|cat`),
  }), [decorSeedBase]);

  const [decorFrames, setDecorFrames] = useState<DecorFrames>(seededDecorFrames);
  const [coffeePaused, setCoffeePaused] = useState(false);
  const [serverMode, setServerMode] = useState<'auto' | 'on' | 'off'>('auto');

  const targetsRef = useRef<{ main: Point; nodes: typeof placedNodes }>({ main: mainTarget, nodes: placedNodes });
  const stateRef = useRef<{ mainState: MainVisualState; nodes: typeof placedNodes }>({ mainState: effectiveMainState, nodes: placedNodes });

  useEffect(() => {
    targetsRef.current = { main: mainTarget, nodes: placedNodes };
    stateRef.current = { mainState: effectiveMainState, nodes: placedNodes };
  }, [mainTarget, placedNodes, effectiveMainState]);

  useEffect(() => {
    setDecorFrames(seededDecorFrames);
  }, [seededDecorFrames]);

  useEffect(() => {
    setNodePos((prev) => {
      const next: Record<string, Point> = {};
      for (const n of placedNodes) {
        next[n.key] = prev[n.key] || n.point;
      }
      return next;
    });
  }, [placedNodes]);

  useEffect(() => {
    const timer = window.setInterval(() => {
      setTick((v) => (v + 1) % 100000000);
      setMainPos((prev) => lerpPoint(prev, targetsRef.current.main, 0.2));
      setNodePos((prev) => {
        const next: Record<string, Point> = {};
        for (const n of targetsRef.current.nodes) {
          const cur = prev[n.key] || n.point;
          next[n.key] = lerpPoint(cur, n.point, 0.24);
        }
        return next;
      });
    }, RENDER_INTERVAL_MS);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    const timer = window.setInterval(() => {
      const now = Date.now();
      setMainBubble((prev) => (prev && prev.expiresAt > now ? prev : null));
      setNodeBubbles((prev) => {
        const next: Record<string, BubbleState> = {};
        for (const [k, v] of Object.entries(prev)) {
          if (v.expiresAt > now) next[k] = v;
        }
        return next;
      });

      if (now < nextBubbleAtRef.current) return;

      const mainPool = MAIN_BUBBLE_TEXTS[stateRef.current.mainState] || MAIN_BUBBLE_TEXTS.idle;
      const mainLine = pickRandom(mainPool);
      if (mainLine) setMainBubble({ text: mainLine, expiresAt: now + 2600 });

      const nextNodeBubbles: Record<string, BubbleState> = {};
      const candidates = [...stateRef.current.nodes];
      const bubbleCount = Math.min(2, candidates.length);
      for (let i = 0; i < bubbleCount; i += 1) {
        const pick = candidates.splice(Math.floor(Math.random() * candidates.length), 1)[0];
        if (!pick) continue;
        const nodeState = normalizeNodeState(pick.state);
        const line = pickRandom(NODE_BUBBLE_TEXTS[nodeState] || NODE_BUBBLE_TEXTS.idle);
        if (line) nextNodeBubbles[pick.key] = { text: line, expiresAt: now + 2300 };
      }
      if (Object.keys(nextNodeBubbles).length > 0) {
        setNodeBubbles((prev) => ({ ...prev, ...nextNodeBubbles }));
      }

      nextBubbleAtRef.current = now + 3200 + Math.floor(Math.random() * 2000);
    }, 450);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    cameraRef.current = camera;
  }, [camera]);

  const updatePointerWorld = useCallback((clientX: number, clientY: number) => {
    const rect = viewportRef.current?.getBoundingClientRect();
    if (!rect) return;
    const px = clamp(clientX - rect.left, 0, rect.width);
    const py = clamp(clientY - rect.top, 0, rect.height);
    const cam = cameraRef.current;
    const viewW = OFFICE_CANVAS.width / cam.zoom;
    const viewH = OFFICE_CANVAS.height / cam.zoom;
    setPointerWorld({
      x: cam.x + (px / Math.max(1, rect.width)) * viewW,
      y: cam.y + (py / Math.max(1, rect.height)) * viewH,
    });
  }, []);

  const handlePointerDown = useCallback((e: React.PointerEvent<HTMLDivElement>) => {
    if (!panEnabled) return;
    panRef.current = { active: true, x: e.clientX, y: e.clientY };
    (e.currentTarget as HTMLDivElement).setPointerCapture?.(e.pointerId);
  }, [panEnabled]);

  const handlePointerUp = useCallback((e: React.PointerEvent<HTMLDivElement>) => {
    panRef.current = null;
    (e.currentTarget as HTMLDivElement).releasePointerCapture?.(e.pointerId);
  }, []);

  const handlePointerMove = useCallback((e: React.PointerEvent<HTMLDivElement>) => {
    updatePointerWorld(e.clientX, e.clientY);
    const rect = viewportRef.current?.getBoundingClientRect();
    const pan = panRef.current;
    if (!rect || !pan || !pan.active || !panEnabled) return;

    const dx = e.clientX - pan.x;
    const dy = e.clientY - pan.y;
    panRef.current = { active: true, x: e.clientX, y: e.clientY };

    setCamera((prev) => {
      const viewW = OFFICE_CANVAS.width / prev.zoom;
      const viewH = OFFICE_CANVAS.height / prev.zoom;
      const worldPerPixelX = viewW / Math.max(1, rect.width);
      const worldPerPixelY = viewH / Math.max(1, rect.height);
      return clampCamera({
        ...prev,
        x: prev.x - dx * worldPerPixelX,
        y: prev.y - dy * worldPerPixelY,
      });
    });
  }, [panEnabled, updatePointerWorld]);

  const zoomBy = useCallback((delta: number) => {
    setCamera((prev) => {
      const nextZoom = clamp(prev.zoom + delta, MIN_ZOOM, MAX_ZOOM);
      const centerX = prev.x + OFFICE_CANVAS.width / (2 * prev.zoom);
      const centerY = prev.y + OFFICE_CANVAS.height / (2 * prev.zoom);
      return clampCamera({
        zoom: nextZoom,
        x: centerX - OFFICE_CANVAS.width / (2 * nextZoom),
        y: centerY - OFFICE_CANVAS.height / (2 * nextZoom),
      });
    });
  }, []);

  const resetView = useCallback(() => {
    setCamera({ x: 0, y: 0, zoom: 1 });
  }, []);

  const setVisualState = useCallback((state: MainVisualState | null) => {
    setManualState(state);
  }, []);

  const cycleServerMode = useCallback(() => {
    setServerMode((prev) => (prev === 'auto' ? 'on' : prev === 'on' ? 'off' : 'auto'));
  }, []);

  const coffeeFrame = coffeePaused ? 0 : frameAtTick(DECOR_SPRITES.coffeeMachine, tick, 300);
  const serverOn = serverMode === 'on' || (serverMode === 'auto' && effectiveMainState !== 'idle');
  const serverFrame = serverOn ? frameAtTick(DECOR_SPRITES.serverroom, tick, 700) : 0;
  const mainFrame = frameAtTick(MAIN_SPRITES[effectiveMainState], tick);

  const furnitureScale = camera.zoom;

  return (
    <div className="relative w-full overflow-hidden rounded-2xl border border-zinc-800 bg-zinc-950/60 select-none">
      <div className="relative aspect-[16/9]">
        <div
          ref={viewportRef}
          className="absolute inset-0 overflow-hidden"
          onPointerDown={handlePointerDown}
          onPointerMove={handlePointerMove}
          onPointerUp={handlePointerUp}
          onPointerCancel={handlePointerUp}
          style={{ touchAction: panEnabled ? 'none' : 'pan-y' }}
        >
          <PlacedImage spec={DECOR_IMAGES.bg} point={{ x: 640, y: 360 }} camera={camera} zIndex={1} scaleMultiplier={furnitureScale} />

          <PlacedSprite
            spec={DECOR_SPRITES.serverroom}
            frame={serverFrame}
            point={{ x: 1021, y: 142 }}
            camera={camera}
            zIndex={10}
            scaleMultiplier={furnitureScale}
            title={`serverroom (${serverMode})`}
            onClick={cycleServerMode}
          />

          <PlacedSprite
            spec={DECOR_SPRITES.posters}
            frame={decorFrames.poster}
            point={{ x: 252, y: 66 }}
            camera={camera}
            zIndex={20}
            scaleMultiplier={furnitureScale}
            title="poster"
            onClick={() => setDecorFrames((v) => ({ ...v, poster: randFrame(DECOR_SPRITES.posters) }))}
          />
          <PlacedSprite
            spec={DECOR_SPRITES.plants}
            frame={decorFrames.plantA}
            point={{ x: 565, y: 178 }}
            camera={camera}
            zIndex={21}
            scaleMultiplier={furnitureScale}
            title="plant A"
            onClick={() => setDecorFrames((v) => ({ ...v, plantA: randFrame(DECOR_SPRITES.plants) }))}
          />
          <PlacedSprite
            spec={DECOR_SPRITES.plants}
            frame={decorFrames.plantB}
            point={{ x: 230, y: 185 }}
            camera={camera}
            zIndex={21}
            scaleMultiplier={furnitureScale}
            title="plant B"
            onClick={() => setDecorFrames((v) => ({ ...v, plantB: randFrame(DECOR_SPRITES.plants) }))}
          />
          <PlacedSprite
            spec={DECOR_SPRITES.plants}
            frame={decorFrames.plantC}
            point={{ x: 977, y: 496 }}
            camera={camera}
            zIndex={21}
            scaleMultiplier={furnitureScale}
            title="plant C"
            onClick={() => setDecorFrames((v) => ({ ...v, plantC: randFrame(DECOR_SPRITES.plants) }))}
          />

          <PlacedImage spec={DECOR_IMAGES.sofaShadow} point={{ x: 1070, y: 610 }} camera={camera} zIndex={25} scaleMultiplier={furnitureScale} />
          <PlacedImage
            spec={DECOR_IMAGES.sofaIdle}
            point={{ x: 1070, y: 610 }}
            camera={camera}
            zIndex={26}
            scaleMultiplier={furnitureScale}
            title="sofa"
            onClick={() => setVisualState('idle')}
          />

          <PlacedImage spec={DECOR_IMAGES.coffeeShadow} point={{ x: 659, y: 397 }} camera={camera} zIndex={30} scaleMultiplier={furnitureScale} />
          <PlacedSprite
            spec={DECOR_SPRITES.coffeeMachine}
            frame={coffeeFrame}
            point={{ x: 659, y: 397 }}
            camera={camera}
            zIndex={31}
            scaleMultiplier={furnitureScale}
            title={coffeePaused ? 'coffee paused' : 'coffee playing'}
            onClick={() => setCoffeePaused((v) => !v)}
          />

          <PlacedImage
            spec={DECOR_IMAGES.desk}
            point={{ x: 218, y: 417 }}
            camera={camera}
            zIndex={35}
            scaleMultiplier={furnitureScale}
            title="desk"
            onClick={() => setVisualState('working')}
          />
          <PlacedSprite
            spec={DECOR_SPRITES.flowers}
            frame={decorFrames.flower}
            point={{ x: 310, y: 390 }}
            camera={camera}
            zIndex={36}
            scaleMultiplier={furnitureScale}
            title="flower"
            onClick={() => setDecorFrames((v) => ({ ...v, flower: randFrame(DECOR_SPRITES.flowers) }))}
          />

          <PlacedSprite
            spec={MAIN_SPRITES[effectiveMainState]}
            frame={mainFrame}
            point={mainPos}
            camera={camera}
            zIndex={50}
            title={`${main.state || 'idle'} ${main.detail || ''}`.trim()}
            scaleMultiplier={furnitureScale}
          />
          <div
            className="absolute -translate-x-1/2 -translate-y-1/2 pointer-events-none"
            style={{
              ...(() => {
                const p = worldToScreen({ x: mainPos.x, y: mainPos.y + 62 }, camera);
                return { left: `${p.x}%`, top: `${p.y}%` };
              })(),
              zIndex: 55,
            }}
          >
            <div className="rounded bg-black/75 px-2 py-0.5 text-[10px] font-semibold tracking-wide text-zinc-100">{main.name || main.id || 'main'}</div>
          </div>

          {mainBubble && mainBubble.expiresAt > Date.now() ? (
            <Bubble point={mainPos} camera={camera} text={mainBubble.text} zIndex={70} />
          ) : null}

          {placedNodes.map((n) => {
            const p = nodePos[n.key] || n.point;
            const nodeFrame = frameAtTick(NODE_SPRITES[n.spriteIndex], tick, n.avatarSeed % 1000);
            const nodeBubble = nodeBubbles[n.key];
            return (
              <React.Fragment key={n.key}>
                <PlacedSprite
                  spec={NODE_SPRITES[n.spriteIndex]}
                  frame={nodeFrame}
                  point={p}
                  camera={camera}
                  zIndex={51}
                  title={`${n.name || n.id || 'node'} · ${n.state || 'idle'}${n.detail ? ` · ${n.detail}` : ''}`}
                  scaleMultiplier={furnitureScale}
                />
                {nodeBubble && nodeBubble.expiresAt > Date.now() ? (
                  <Bubble point={p} camera={camera} text={nodeBubble.text} zIndex={71} />
                ) : null}
              </React.Fragment>
            );
          })}

          <PlacedSprite
            spec={DECOR_SPRITES.cats}
            frame={decorFrames.cat}
            point={{ x: 94, y: 557 }}
            camera={camera}
            zIndex={80}
            scaleMultiplier={furnitureScale}
            title="cat"
            onClick={() => setDecorFrames((v) => ({ ...v, cat: randFrame(DECOR_SPRITES.cats) }))}
          />

          {showDebug ? (
            <>
              {Object.entries(OFFICE_ZONE_POINT).map(([zone, p]) => {
                const s = worldToScreen(p, camera);
                return (
                  <div key={`zone-${zone}`} className="absolute -translate-x-1/2 -translate-y-1/2 pointer-events-none" style={{ left: `${s.x}%`, top: `${s.y}%`, zIndex: 200 }}>
                    <div className="h-2 w-2 rounded-full bg-yellow-400" />
                    <div className="mt-1 rounded bg-black/70 px-1 text-[10px] text-yellow-200">{zone}</div>
                  </div>
                );
              })}
              {Object.entries(OFFICE_ZONE_SLOTS).map(([zone, slots]) =>
                slots.map((p, idx) => {
                  const s = worldToScreen(p, camera);
                  return (
                    <div
                      key={`slot-${zone}-${idx}`}
                      className="absolute -translate-x-1/2 -translate-y-1/2 pointer-events-none"
                      style={{ left: `${s.x}%`, top: `${s.y}%`, zIndex: 190 }}
                    >
                      <div className="h-1.5 w-1.5 rounded-full bg-cyan-300/90" />
                    </div>
                  );
                })
              )}
            </>
          ) : null}
        </div>

        <div className="absolute left-2 top-2 z-[300] flex flex-wrap gap-1.5 rounded-lg border border-zinc-700 bg-zinc-950/85 p-2 text-[11px]">
          <button className="rounded bg-zinc-800 px-2 py-1 hover:bg-zinc-700" onClick={() => setVisualState(null)}>
            Follow
          </button>
          <button className="rounded bg-zinc-800 px-2 py-1 hover:bg-zinc-700" onClick={() => setVisualState('idle')}>
            Idle
          </button>
          <button className="rounded bg-zinc-800 px-2 py-1 hover:bg-zinc-700" onClick={() => setVisualState('working')}>
            Work
          </button>
          <button className="rounded bg-zinc-800 px-2 py-1 hover:bg-zinc-700" onClick={() => setVisualState('syncing')}>
            Sync
          </button>
          <button className="rounded bg-zinc-800 px-2 py-1 hover:bg-zinc-700" onClick={() => setVisualState('error')}>
            Error
          </button>
          <button className="rounded bg-zinc-800 px-2 py-1 hover:bg-zinc-700" onClick={() => setPanEnabled((v) => !v)}>
            {panEnabled ? 'Pan On' : 'Pan Off'}
          </button>
          <button className="rounded bg-zinc-800 px-2 py-1 hover:bg-zinc-700" onClick={() => zoomBy(0.2)}>
            Zoom+
          </button>
          <button className="rounded bg-zinc-800 px-2 py-1 hover:bg-zinc-700" onClick={() => zoomBy(-0.2)}>
            Zoom-
          </button>
          <button className="rounded bg-zinc-800 px-2 py-1 hover:bg-zinc-700" onClick={resetView}>
            Reset View
          </button>
          <button className="rounded bg-zinc-800 px-2 py-1 hover:bg-zinc-700" onClick={() => setShowDebug((v) => !v)}>
            {showDebug ? 'Debug Off' : 'Debug On'}
          </button>
        </div>

        <div className="absolute right-2 top-2 z-[300] rounded-lg border border-zinc-700 bg-zinc-950/85 px-2 py-1 text-[11px] text-zinc-300">
          state={effectiveMainState} live={liveMainState} mode={manualState ? 'manual' : 'follow'} server={serverMode} coffee={coffeePaused ? 'paused' : 'on'}
        </div>

        {showDebug ? (
          <div className="absolute bottom-2 left-2 z-[300] rounded-lg border border-zinc-700 bg-zinc-950/85 px-2 py-1 text-[11px] text-zinc-300">
            cam x={camera.x.toFixed(1)} y={camera.y.toFixed(1)} zoom={camera.zoom.toFixed(2)}
            {pointerWorld ? ` | pointer ${Math.round(pointerWorld.x)},${Math.round(pointerWorld.y)}` : ''}
            {` | nodes ${placedNodes.length}`}
          </div>
        ) : null}
      </div>
    </div>
  );
};

export default OfficeScene;
