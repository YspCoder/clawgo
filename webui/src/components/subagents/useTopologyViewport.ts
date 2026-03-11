import { useEffect, useRef, useState } from 'react';

type NodePosition = {
  key: string;
  x: number;
  y: number;
};

type UseTopologyViewportParams = {
  clearTooltip: () => void;
};

type TopologyDragRefState = {
  active: boolean;
  startX: number;
  startY: number;
  panX: number;
  panY: number;
};

type NodeDragRefState = {
  startX: number;
  startY: number;
  initialNodeX: number;
  initialNodeY: number;
};

export function useTopologyViewport({
  clearTooltip,
}: UseTopologyViewportParams) {
  const [topologyZoom, setTopologyZoom] = useState(0.9);
  const [topologyPan, setTopologyPan] = useState({ x: 0, y: 0 });
  const [nodeOverrides, setNodeOverrides] = useState<Record<string, { x: number; y: number }>>({});
  const [draggedNode, setDraggedNode] = useState<string | null>(null);
  const [topologyDragging, setTopologyDragging] = useState(false);

  const topologyViewportRef = useRef<HTMLDivElement | null>(null);
  const topologyDragRef = useRef<TopologyDragRefState>({
    active: false,
    startX: 0,
    startY: 0,
    panX: 0,
    panY: 0,
  });
  const nodeDragRef = useRef<NodeDragRefState>({
    startX: 0,
    startY: 0,
    initialNodeX: 0,
    initialNodeY: 0,
  });

  const fitView = (width: number, height: number) => {
    const viewport = topologyViewportRef.current;
    if (!viewport || !width) return;
    const availableW = viewport.clientWidth;
    const availableH = viewport.clientHeight;
    const fitted = Math.min(1.15, Math.max(0.2, (availableW - 48) / width));

    setTopologyZoom(fitted);
    setTopologyPan({
      x: (availableW - width * fitted) / 2,
      y: Math.max(24, (availableH - height * fitted) / 2),
    });
  };

  useEffect(() => {
    const viewport = topologyViewportRef.current;
    if (!viewport) return;

    const handleWheel = (event: WheelEvent) => {
      if (!(event.ctrlKey || event.metaKey)) {
        return;
      }
      event.preventDefault();

      const rect = viewport.getBoundingClientRect();
      const mouseX = event.clientX - rect.left;
      const mouseY = event.clientY - rect.top;

      setTopologyZoom((prevZoom) => {
        const delta = -event.deltaY * 0.002;
        const newZoom = Math.min(Math.max(0.1, prevZoom * (1 + delta)), 4);

        setTopologyPan((prevPan) => {
          const scaleRatio = newZoom / prevZoom;
          return {
            x: mouseX - (mouseX - prevPan.x) * scaleRatio,
            y: mouseY - (mouseY - prevPan.y) * scaleRatio,
          };
        });

        return newZoom;
      });
    };

    viewport.addEventListener('wheel', handleWheel, { passive: false });
    return () => viewport.removeEventListener('wheel', handleWheel);
  }, []);

  const handleNodeDragStart = (
    key: string,
    event: React.MouseEvent<HTMLDivElement>,
    resolveNodePosition: (cardKey: string) => NodePosition | null,
  ) => {
    if (event.button !== 0) return;
    event.stopPropagation();

    const card = resolveNodePosition(key);
    if (!card) return;

    setDraggedNode(key);
    nodeDragRef.current = {
      startX: event.clientX,
      startY: event.clientY,
      initialNodeX: card.x,
      initialNodeY: card.y,
    };
    clearTooltip();
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
    clearTooltip();
  };

  const moveTopologyDrag = (event: React.MouseEvent<HTMLDivElement>) => {
    if (draggedNode) {
      const deltaX = (event.clientX - nodeDragRef.current.startX) / topologyZoom;
      const deltaY = (event.clientY - nodeDragRef.current.startY) / topologyZoom;

      setNodeOverrides((prev) => ({
        ...prev,
        [draggedNode]: {
          x: nodeDragRef.current.initialNodeX + deltaX,
          y: nodeDragRef.current.initialNodeY + deltaY,
        },
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

  const zoomTopologyAroundCenter = (newZoom: number) => {
    const viewport = topologyViewportRef.current;
    if (viewport) {
      const rect = viewport.getBoundingClientRect();
      const mouseX = rect.width / 2;
      const mouseY = rect.height / 2;
      const scaleRatio = newZoom / topologyZoom;
      setTopologyPan((prev) => ({
        x: mouseX - (mouseX - prev.x) * scaleRatio,
        y: mouseY - (mouseY - prev.y) * scaleRatio,
      }));
    }
    setTopologyZoom(newZoom);
  };

  const handleTopologyZoomOut = () => {
    const newZoom = Math.max(0.1, Number((topologyZoom - 0.1).toFixed(2)));
    zoomTopologyAroundCenter(newZoom);
  };

  const handleTopologyResetZoom = () => {
    zoomTopologyAroundCenter(1);
  };

  const handleTopologyZoomIn = () => {
    const newZoom = Math.min(4, Number((topologyZoom + 0.1).toFixed(2)));
    zoomTopologyAroundCenter(newZoom);
  };

  return {
    draggedNode,
    handleNodeDragStart,
    handleTopologyResetZoom,
    handleTopologyZoomIn,
    handleTopologyZoomOut,
    fitView,
    moveTopologyDrag,
    nodeOverrides,
    setNodeOverrides,
    startTopologyDrag,
    stopTopologyDrag,
    topologyDragging,
    topologyPan,
    topologyViewportRef,
    topologyZoom,
  };
}
