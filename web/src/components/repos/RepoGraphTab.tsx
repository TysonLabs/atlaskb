import { useEffect, useState, useCallback, useMemo, useRef } from "react";
import ForceGraph2D, { type ForceGraphMethods, type NodeObject, type LinkObject } from "react-force-graph-2d";
import { api } from "../../api/client";
import type { GraphData } from "../../types";
import { Loader2, ZoomIn, ZoomOut, Maximize2, Focus, X } from "lucide-react";

const kindColors: Record<string, string> = {
  module: "#61afef",
  service: "#98c379",
  function: "#c678dd",
  type: "#d19a66",
  endpoint: "#e06c75",
  concept: "#545964",
  config: "#e5c07b",
  cluster: "#56b6c2",
};

const relKindColors: Record<string, string> = {
  calls: "#61afef",
  depends_on: "#e06c75",
  implements: "#98c379",
  contains: "#545964",
  member_of: "#56b6c2",
  extends: "#c678dd",
  emits: "#e5c07b",
  reads: "#d19a66",
  writes: "#e06c75",
  consumes: "#d19a66",
};

const relKindLabels: Record<string, string> = {
  calls: "Calls",
  consumes: "Consumes",
  depends_on: "Depends On",
  implements: "Implements",
  extends: "Extends",
  contains: "Contains",
  emits: "Emits",
  reads: "Reads",
  writes: "Writes",
  member_of: "Member Of",
};

interface GNode {
  id: string;
  name: string;
  kind: string;
  val: number;
}

interface GLink {
  source: string;
  target: string;
  kind: string;
  strength: string;
}

interface Props {
  repoId: string;
  onEntityClick: (id: string) => void;
  selectedEntityId?: string;
  focusEntityId?: string;
  onDeselect?: () => void;
  highlightedEntityIds?: Set<string>;
}

const depthOptions = [
  { label: "All", value: null },
  { label: "1 hop", value: 1 },
  { label: "2 hops", value: 2 },
  { label: "3 hops", value: 3 },
  { label: "5 hops", value: 5 },
] as const;

export function RepoGraphTab({ repoId, onEntityClick, selectedEntityId, focusEntityId, onDeselect, highlightedEntityIds }: Props) {
  const [rawData, setRawData] = useState<GraphData | null>(null);
  const [loading, setLoading] = useState(true);
  const [hiddenKinds, setHiddenKinds] = useState<Set<string>>(new Set());
  const [hiddenRelKinds, setHiddenRelKinds] = useState<Set<string>>(new Set());
  const [dimensions, setDimensions] = useState({ width: 800, height: 500 });
  const [hoveredNode, setHoveredNode] = useState<GNode | null>(null);
  const [depthFilter, setDepthFilter] = useState<number | null>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const fgRef = useRef<ForceGraphMethods<NodeObject<GNode>, LinkObject<GNode, GLink>>>(undefined);

  useEffect(() => {
    setLoading(true);
    api.getRepoGraph(repoId)
      .then(setRawData)
      .catch(console.error)
      .finally(() => setLoading(false));
  }, [repoId]);

  useEffect(() => {
    if (!containerRef.current) return;
    const ro = new ResizeObserver(([entry]) => {
      setDimensions({
        width: entry.contentRect.width,
        height: Math.max(entry.contentRect.height, 400),
      });
    });
    ro.observe(containerRef.current);
    return () => ro.disconnect();
  }, []);

  const allEntityKinds = useMemo(() => {
    if (!rawData) return [];
    return [...new Set(rawData.nodes.map((n) => n.kind))].sort();
  }, [rawData]);

  const allRelKinds = useMemo(() => {
    if (!rawData) return [];
    return [...new Set(rawData.edges.map((e) => e.kind))].sort();
  }, [rawData]);

  // BFS subgraph computation for depth filter
  const depthFilteredIds = useMemo(() => {
    if (depthFilter == null || !selectedEntityId || !rawData) return null;

    // Build adjacency list from raw edges (before filtering)
    const adj = new Map<string, Set<string>>();
    rawData.edges.forEach((e) => {
      if (!adj.has(e.source)) adj.set(e.source, new Set());
      if (!adj.has(e.target)) adj.set(e.target, new Set());
      adj.get(e.source)!.add(e.target);
      adj.get(e.target)!.add(e.source);
    });

    const visited = new Set<string>();
    const queue: Array<{ id: string; depth: number }> = [{ id: selectedEntityId, depth: 0 }];
    visited.add(selectedEntityId);

    while (queue.length > 0) {
      const { id, depth } = queue.shift()!;
      if (depth >= depthFilter) continue;
      const neighbors = adj.get(id);
      if (!neighbors) continue;
      for (const neighbor of neighbors) {
        if (!visited.has(neighbor)) {
          visited.add(neighbor);
          queue.push({ id: neighbor, depth: depth + 1 });
        }
      }
    }

    return visited;
  }, [depthFilter, selectedEntityId, rawData]);

  const graphData = useMemo(() => {
    if (!rawData) return { nodes: [], links: [] };
    let visibleNodes = rawData.nodes.filter((n) => !hiddenKinds.has(n.kind));

    // Apply depth filter if active
    if (depthFilteredIds) {
      visibleNodes = visibleNodes.filter((n) => depthFilteredIds.has(n.id));
    }

    const visibleIds = new Set(visibleNodes.map((n) => n.id));
    const nodes: GNode[] = visibleNodes.map((n) => ({
      id: n.id,
      name: n.name,
      kind: n.kind,
      val: 2,
    }));
    const links: GLink[] = rawData.edges
      .filter((e) => !hiddenRelKinds.has(e.kind) && visibleIds.has(e.source) && visibleIds.has(e.target))
      .map((e) => ({ source: e.source, target: e.target, kind: e.kind, strength: e.strength }));
    return { nodes, links };
  }, [rawData, hiddenKinds, hiddenRelKinds, depthFilteredIds]);

  // Find the selected node from graphData
  const selectedNode = useMemo(() => {
    if (!selectedEntityId) return null;
    return graphData.nodes.find((n) => n.id === selectedEntityId) || null;
  }, [selectedEntityId, graphData]);

  // D3 force config with requestAnimationFrame, dependent on rawData
  useEffect(() => {
    const rafId = requestAnimationFrame(() => {
      if (!fgRef.current) return;
      fgRef.current.d3Force("charge")?.strength(-150);
      fgRef.current.d3Force("link")?.distance(60);
    });
    return () => cancelAnimationFrame(rafId);
  }, [rawData]);

  // Reset depth filter when selected entity clears
  useEffect(() => {
    if (!selectedEntityId) setDepthFilter(null);
  }, [selectedEntityId]);

  // Focus camera on entity when focusEntityId changes (with retry for unmounted graphs)
  useEffect(() => {
    if (!focusEntityId || !fgRef.current) return;
    let attempts = 0;
    const tryFocus = () => {
      if (attempts > 20 || !fgRef.current) return;
      const node = fgRef.current.graphData().nodes.find((n: any) => n.id === focusEntityId);
      if (node && node.x != null && node.y != null) {
        fgRef.current.centerAt(node.x, node.y, 400);
        fgRef.current.zoom(4, 400);
      } else {
        attempts++;
        requestAnimationFrame(tryFocus);
      }
    };
    // Small initial delay to let ForceGraph mount
    const id = requestAnimationFrame(tryFocus);
    return () => cancelAnimationFrame(id);
  }, [focusEntityId, graphData]);

  const toggleKind = (kind: string) => {
    setHiddenKinds((prev) => {
      const next = new Set(prev);
      if (next.has(kind)) next.delete(kind);
      else next.add(kind);
      return next;
    });
  };

  const toggleRelKind = (kind: string) => {
    setHiddenRelKinds((prev) => {
      const next = new Set(prev);
      if (next.has(kind)) next.delete(kind);
      else next.add(kind);
      return next;
    });
  };

  const paintNode = useCallback(
    (node: NodeObject<GNode>, ctx: CanvasRenderingContext2D) => {
      const { x = 0, y = 0 } = node;
      const color = kindColors[node.kind] || "#545964";
      const isSelected = selectedEntityId === node.id;
      const r = isSelected ? 7 : 5;

      // Determine if this node should be dimmed due to highlight filtering
      const hasHighlight = highlightedEntityIds && highlightedEntityIds.size > 0;
      const isHighlighted = hasHighlight && highlightedEntityIds!.has(node.id);
      const isDimmed = hasHighlight && !highlightedEntityIds!.has(node.id);

      if (isDimmed) {
        ctx.globalAlpha = 0.2;
      }

      // Highlighted node glow
      if (isHighlighted && !isSelected) {
        ctx.beginPath();
        ctx.arc(x, y, r + 4, 0, 2 * Math.PI);
        ctx.fillStyle = color;
        ctx.globalAlpha = isDimmed ? 0.05 : 0.15;
        ctx.fill();
        ctx.globalAlpha = isDimmed ? 0.2 : 1;
      }

      // Selected node glow ring
      if (isSelected) {
        ctx.beginPath();
        ctx.arc(x, y, r + 3, 0, 2 * Math.PI);
        ctx.strokeStyle = color;
        ctx.lineWidth = 1.5;
        ctx.globalAlpha = isDimmed ? 0.1 : 0.5;
        ctx.stroke();
        ctx.globalAlpha = isDimmed ? 0.2 : 1;
      }

      ctx.beginPath();
      ctx.arc(x, y, r, 0, 2 * Math.PI);
      ctx.fillStyle = color;
      ctx.fill();
      ctx.font = "3px sans-serif";
      ctx.textAlign = "center";
      ctx.fillStyle = "#abb2bf";
      ctx.fillText(node.name, x, y + r + 4);

      // Reset alpha
      ctx.globalAlpha = 1;
    },
    [selectedEntityId, highlightedEntityIds],
  );

  const paintNodePointerArea = useCallback(
    (node: NodeObject<GNode>, color: string, ctx: CanvasRenderingContext2D) => {
      const { x = 0, y = 0 } = node;
      ctx.beginPath();
      ctx.arc(x, y, 8, 0, 2 * Math.PI);
      ctx.fillStyle = color;
      ctx.fill();
    },
    [],
  );

  const handleNodeClick = useCallback(
    (node: NodeObject<GNode>) => {
      onEntityClick(node.id);
    },
    [onEntityClick],
  );

  const handleBackgroundClick = useCallback(() => {
    onDeselect?.();
    fgRef.current?.zoomToFit(300, 40);
  }, [onDeselect]);

  const handleNodeHover = useCallback(
    (node: NodeObject<GNode> | null) => {
      setHoveredNode(node ? { id: node.id, name: node.name, kind: node.kind, val: node.val } : null);
      // Set cursor on the canvas element
      const canvas = containerRef.current?.querySelector("canvas");
      if (canvas) {
        canvas.style.cursor = node ? "pointer" : "default";
      }
    },
    [],
  );

  const getLinkColor = useCallback(
    (link: GLink) => {
      return relKindColors[link.kind] || "#3a3f4b";
    },
    [],
  );

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 size={24} className="animate-spin text-foreground-muted" />
      </div>
    );
  }

  if (!rawData || rawData.nodes.length === 0) {
    return (
      <div className="bg-surface-elevated rounded-lg border border-edge p-6 text-center">
        <p className="text-sm text-foreground-secondary">No graph data available — re-index to generate.</p>
      </div>
    );
  }

  return (
    <div className="space-y-3">
      {/* Filter controls */}
      <div className="flex flex-wrap gap-4">
        <div>
          <p className="text-xs text-foreground-secondary mb-1">Entity kinds</p>
          <div className="flex flex-wrap gap-1">
            {allEntityKinds.map((kind) => {
              const hidden = hiddenKinds.has(kind);
              return (
                <button
                  key={kind}
                  onClick={() => toggleKind(kind)}
                  className={`text-xs px-2 py-0.5 rounded-full transition-colors ${
                    hidden
                      ? "bg-surface-overlay text-foreground-muted line-through"
                      : "text-white font-medium"
                  }`}
                  style={!hidden ? { backgroundColor: kindColors[kind] || "#545964" } : undefined}
                >
                  {kind}
                </button>
              );
            })}
          </div>
        </div>
        <div>
          <p className="text-xs text-foreground-secondary mb-1">Relationship kinds</p>
          <div className="flex flex-wrap gap-1">
            {allRelKinds.map((kind) => {
              const hidden = hiddenRelKinds.has(kind);
              return (
                <button
                  key={kind}
                  onClick={() => toggleRelKind(kind)}
                  className={`flex items-center gap-1.5 text-xs px-2 py-0.5 rounded-full transition-colors ${
                    hidden
                      ? "bg-surface-overlay text-foreground-muted line-through"
                      : "bg-surface-overlay text-foreground-secondary"
                  }`}
                >
                  <span
                    className="inline-block h-1 w-4 rounded-full"
                    style={{ backgroundColor: relKindColors[kind] || "#3a3f4b" }}
                  />
                  {relKindLabels[kind] || kind}
                </button>
              );
            })}
          </div>
        </div>
        {/* Depth filter */}
        <div>
          <p className="text-xs text-foreground-secondary mb-1">
            Depth {!selectedEntityId && <span className="text-foreground-muted">(select a node)</span>}
          </p>
          <div className="flex flex-wrap gap-1">
            {depthOptions.map((opt) => {
              const active = depthFilter === opt.value;
              return (
                <button
                  key={opt.label}
                  onClick={() => setDepthFilter(opt.value)}
                  disabled={!selectedEntityId && opt.value !== null}
                  className={`text-xs px-2 py-0.5 rounded-full transition-colors ${
                    active
                      ? "bg-accent text-white font-medium"
                      : "bg-surface-overlay text-foreground-secondary hover:text-foreground"
                  } ${!selectedEntityId && opt.value !== null ? "opacity-40 cursor-not-allowed" : ""}`}
                >
                  {opt.label}
                </button>
              );
            })}
          </div>
        </div>
      </div>

      {/* Graph container */}
      <div ref={containerRef} className="relative bg-surface rounded-lg border border-edge overflow-hidden" style={{ height: 500 }}>
        {/* Selected node status bar (priority over hover tooltip) */}
        {selectedNode && (
          <div className="absolute top-3 left-1/2 -translate-x-1/2 flex items-center gap-2 px-4 py-2 bg-accent/15 border border-accent/30 rounded-xl backdrop-blur-sm z-10 animate-in slide-in-from-top duration-200">
            <div className="w-2 h-2 bg-accent rounded-full animate-pulse" />
            <span className="font-mono text-sm text-foreground">{selectedNode.name}</span>
            <span className="text-xs text-foreground-muted">({selectedNode.kind})</span>
            <button onClick={() => onDeselect?.()} className="ml-1 text-foreground-muted hover:text-foreground" title="Clear selection">
              <X size={14} />
            </button>
          </div>
        )}

        {/* Hover tooltip (only when no selected node bar is showing) */}
        {hoveredNode && !selectedNode && (
          <div className="absolute top-3 left-1/2 -translate-x-1/2 px-3 py-1.5 bg-surface-elevated/95 border border-edge rounded-lg backdrop-blur-sm z-10 pointer-events-none animate-in fade-in duration-150">
            <span className="font-mono text-sm text-foreground">{hoveredNode.name}</span>
            <span className="text-xs text-foreground-muted ml-2">({hoveredNode.kind})</span>
          </div>
        )}

        {/* Zoom controls */}
        <div className="absolute bottom-4 right-4 flex flex-col gap-1 z-10">
          <button
            onClick={() => {
              const fg = fgRef.current;
              if (fg) fg.zoom(fg.zoom() * 1.5, 300);
            }}
            className="p-1.5 bg-surface-elevated/90 border border-edge rounded-md hover:bg-surface-overlay text-foreground-secondary hover:text-foreground transition-colors backdrop-blur-sm"
            title="Zoom in"
          >
            <ZoomIn size={16} />
          </button>
          <button
            onClick={() => {
              const fg = fgRef.current;
              if (fg) fg.zoom(fg.zoom() / 1.5, 300);
            }}
            className="p-1.5 bg-surface-elevated/90 border border-edge rounded-md hover:bg-surface-overlay text-foreground-secondary hover:text-foreground transition-colors backdrop-blur-sm"
            title="Zoom out"
          >
            <ZoomOut size={16} />
          </button>
          <button
            onClick={() => fgRef.current?.zoomToFit(300, 40)}
            className="p-1.5 bg-surface-elevated/90 border border-edge rounded-md hover:bg-surface-overlay text-foreground-secondary hover:text-foreground transition-colors backdrop-blur-sm"
            title="Fit to screen"
          >
            <Maximize2 size={16} />
          </button>
          {selectedEntityId && (
            <>
              <div className="h-px bg-edge" />
              <button
                onClick={() => {
                  const fg = fgRef.current;
                  if (!fg) return;
                  const node = fg.graphData().nodes.find((n: any) => n.id === selectedEntityId);
                  if (node && node.x != null && node.y != null) {
                    fg.centerAt(node.x, node.y, 400);
                    fg.zoom(4, 400);
                  }
                }}
                className="p-1.5 bg-surface-elevated/90 border border-edge rounded-md hover:bg-surface-overlay text-foreground-secondary hover:text-foreground transition-colors backdrop-blur-sm"
                title="Focus selected"
              >
                <Focus size={16} />
              </button>
            </>
          )}
        </div>

        <ForceGraph2D
          ref={fgRef}
          graphData={graphData}
          width={dimensions.width}
          height={dimensions.height}
          backgroundColor="#24272e"
          nodeCanvasObject={paintNode}
          nodeCanvasObjectMode={() => "replace"}
          nodePointerAreaPaint={paintNodePointerArea}
          linkColor={getLinkColor}
          linkWidth={1}
          linkDirectionalArrowLength={4}
          linkDirectionalArrowRelPos={1}
          linkDirectionalArrowColor={getLinkColor}
          linkCurvature={0.15}
          linkLabel={(link: GLink) => `${link.kind} (${link.strength})`}
          onNodeClick={handleNodeClick}
          onNodeHover={handleNodeHover}
          onBackgroundClick={handleBackgroundClick}
          cooldownTicks={150}
          enableNodeDrag={true}
          d3AlphaDecay={0.02}
          d3VelocityDecay={0.3}
        />
      </div>
    </div>
  );
}
