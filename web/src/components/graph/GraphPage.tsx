import { useEffect, useState, useCallback, useMemo, useRef } from "react";
import { useSearchParams } from "react-router-dom";
import ForceGraph2D, { type ForceGraphMethods, type NodeObject, type LinkObject } from "react-force-graph-2d";
import { api } from "../../api/client";
import type { RepoListItem, GraphData, GraphNode } from "../../types";
import { CreateLinkModal } from "./CreateLinkModal";
import { Eye, EyeOff, Package, Maximize2, Link, X } from "lucide-react";

const kindColors: Record<string, string> = {
  module: "#61afef",
  service: "#98c379",
  function: "#c678dd",
  type: "#d19a66",
  endpoint: "#e06c75",
  concept: "#545964",
  config: "#e5c07b",
};

const CROSS_REPO_EDGE_COLOR = "#ff79c6";

// Distinct colors for repos in multi-repo view
const repoColors = [
  "#61afef", "#98c379", "#e06c75", "#d19a66", "#c678dd",
  "#e5c07b", "#56b6c2", "#be5046", "#7c8ca1", "#ff79c6",
];

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
};

interface GNode {
  id: string;
  name: string;
  kind: string;
  path?: string;
  repoId?: string;
  repoName?: string;
  isExternal: boolean;
  group: string;
  val: number;
}

interface GLink {
  source: string;
  target: string;
  kind: string;
  strength: string;
  description?: string;
  crossRepo?: boolean;
}

export function GraphPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const [repos, setRepos] = useState<RepoListItem[]>([]);
  const [rawData, setRawData] = useState<GraphData | null>(null);
  const [selectedNode, setSelectedNode] = useState<GraphNode | null>(null);
  const [hoveredNode, setHoveredNode] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [dimensions, setDimensions] = useState({ width: 800, height: 600 });
  const containerRef = useRef<HTMLDivElement>(null);
  const fgRef = useRef<ForceGraphMethods<NodeObject<GNode>, LinkObject<GNode, GLink>>>(undefined);

  // Filters
  const [showExtDeps, setShowExtDeps] = useState(false);
  const [hiddenKinds, setHiddenKinds] = useState<Set<string>>(new Set());
  const [hiddenRelKinds, setHiddenRelKinds] = useState<Set<string>>(new Set());

  // Multi-repo selection
  const [selectedRepoIds, setSelectedRepoIds] = useState<Set<string>>(new Set());
  const isMultiRepo = selectedRepoIds.size > 1;

  // Link creation mode
  const [linkMode, setLinkMode] = useState(false);
  const [linkFromNode, setLinkFromNode] = useState<GraphNode | null>(null);
  const [showLinkModal, setShowLinkModal] = useState(false);
  const [linkToNode, setLinkToNode] = useState<GraphNode | null>(null);

  const entityId = searchParams.get("entity") || "";

  // Build repo color map
  const repoColorMap = useMemo(() => {
    const map = new Map<string, string>();
    repos.forEach((r, i) => {
      map.set(r.id, repoColors[i % repoColors.length]);
    });
    return map;
  }, [repos]);

  // Resize observer
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const obs = new ResizeObserver((entries) => {
      const { width, height } = entries[0].contentRect;
      setDimensions({ width, height });
    });
    obs.observe(el);
    return () => obs.disconnect();
  }, []);

  useEffect(() => {
    api.listRepos().then(setRepos).catch(console.error);
  }, []);

  // Initialize selectedRepoIds from URL
  useEffect(() => {
    const repoParam = searchParams.get("repo");
    const reposParam = searchParams.get("repos");
    if (reposParam) {
      setSelectedRepoIds(new Set(reposParam.split(",")));
    } else if (repoParam) {
      setSelectedRepoIds(new Set([repoParam]));
    }
  }, []);

  const loadGraph = useCallback(async () => {
    if (selectedRepoIds.size === 0 && !entityId) return;
    setLoading(true);
    try {
      let data: GraphData;
      if (entityId) {
        data = await api.getEntityGraph(entityId, 2);
      } else if (selectedRepoIds.size > 1) {
        data = await api.getMultiRepoGraph([...selectedRepoIds]);
      } else {
        const repoId = [...selectedRepoIds][0];
        data = await api.getRepoGraph(repoId, undefined, undefined);
      }
      setRawData(data);
    } catch (err) {
      console.error(err);
    } finally {
      setLoading(false);
    }
  }, [selectedRepoIds, entityId]);

  useEffect(() => {
    loadGraph();
  }, [loadGraph]);

  // Handle ESC to cancel link mode
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === "Escape" && linkMode) {
        setLinkMode(false);
        setLinkFromNode(null);
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [linkMode]);

  const toggleRepo = (repoId: string) => {
    setSelectedRepoIds((prev) => {
      const next = new Set(prev);
      if (next.has(repoId)) {
        next.delete(repoId);
      } else {
        next.add(repoId);
      }
      // Update URL
      if (next.size === 0) {
        setSearchParams({});
      } else if (next.size === 1) {
        setSearchParams({ repo: [...next][0] });
      } else {
        setSearchParams({ repos: [...next].join(",") });
      }
      return next;
    });
    setSelectedNode(null);
    setHoveredNode(null);
  };

  // Derive available kinds
  const availableEntityKinds = useMemo(() => {
    if (!rawData) return [];
    return [...new Set(rawData.nodes.map((n) => n.kind))].sort();
  }, [rawData]);

  const availableRelKinds = useMemo(() => {
    if (!rawData) return [];
    return [...new Set(rawData.edges.map((e) => e.kind))].sort();
  }, [rawData]);

  // Build filtered graph data for force-graph
  const graphData = useMemo(() => {
    if (!rawData) return { nodes: [] as GNode[], links: [] as GLink[] };

    const relCount = new Map<string, number>();
    rawData.edges.forEach((e) => {
      relCount.set(e.source, (relCount.get(e.source) || 0) + 1);
      relCount.set(e.target, (relCount.get(e.target) || 0) + 1);
    });

    const filteredNodes = rawData.nodes
      .filter((n) => {
        if (!showExtDeps && n.kind === "module" && !n.path) return false;
        if (hiddenKinds.has(n.kind)) return false;
        return true;
      })
      .map((n): GNode => ({
        id: n.id,
        name: n.name,
        kind: n.kind,
        path: n.path,
        repoId: n.repoId,
        repoName: n.repoName,
        isExternal: n.kind === "module" && !n.path,
        group: n.path ? n.path.split("/").slice(0, -1).join("/") || "__root__" : "__external__",
        val: Math.max(2, (relCount.get(n.id) || 1) * 1.5),
      }));

    const visibleIds = new Set(filteredNodes.map((n) => n.id));

    const filteredLinks = rawData.edges
      .filter((e) => {
        if (hiddenRelKinds.has(e.kind)) return false;
        if (!visibleIds.has(e.source) || !visibleIds.has(e.target)) return false;
        return true;
      })
      .map((e): GLink => ({
        source: e.source,
        target: e.target,
        kind: e.kind,
        strength: e.strength,
        description: e.description,
        crossRepo: e.crossRepo,
      }));

    const connectedIds = new Set<string>();
    filteredLinks.forEach((l) => {
      connectedIds.add(l.source);
      connectedIds.add(l.target);
    });

    return {
      nodes: filteredNodes.filter((n) => connectedIds.has(n.id)),
      links: filteredLinks,
    };
  }, [rawData, showExtDeps, hiddenKinds, hiddenRelKinds]);

  // Set up clustering force when graph data changes
  useEffect(() => {
    const fg = fgRef.current;
    if (!fg) return;

    const groupNodes = new Map<string, GNode[]>();
    graphData.nodes.forEach((n) => {
      if (!groupNodes.has(n.group)) groupNodes.set(n.group, []);
      groupNodes.get(n.group)!.push(n);
    });

    fg.d3Force("charge")?.strength(-200);
    fg.d3Force("link")?.distance(80);

    fg.d3Force("cluster", (alpha: number) => {
      for (const group of groupNodes.values()) {
        if (group.length < 2) continue;
        let cx = 0, cy = 0, count = 0;
        for (const n of group) {
          const node = n as NodeObject<GNode>;
          if (node.x != null && node.y != null) {
            cx += node.x;
            cy += node.y;
            count++;
          }
        }
        if (count === 0) continue;
        cx /= count;
        cy /= count;
        for (const n of group) {
          const node = n as NodeObject<GNode>;
          if (node.x != null && node.y != null) {
            node.vx = (node.vx || 0) + (cx - node.x) * alpha * 0.3;
            node.vy = (node.vy || 0) + (cy - node.y) * alpha * 0.3;
          }
        }
      }
    });

    fg.d3ReheatSimulation();
  }, [graphData]);

  // Compute connected node IDs for hover highlighting
  const neighborMap = useMemo(() => {
    const map = new Map<string, Set<string>>();
    graphData.links.forEach((l) => {
      const src = typeof l.source === "object" ? (l.source as any).id : l.source;
      const tgt = typeof l.target === "object" ? (l.target as any).id : l.target;
      if (!map.has(src)) map.set(src, new Set());
      if (!map.has(tgt)) map.set(tgt, new Set());
      map.get(src)!.add(tgt);
      map.get(tgt)!.add(src);
    });
    return map;
  }, [graphData]);

  const isHighlighted = useCallback(
    (nodeId: string) => {
      if (!hoveredNode) return true;
      if (nodeId === hoveredNode) return true;
      return neighborMap.get(hoveredNode)?.has(nodeId) || false;
    },
    [hoveredNode, neighborMap]
  );

  const isLinkHighlighted = useCallback(
    (link: LinkObject<GNode, GLink>) => {
      if (!hoveredNode) return true;
      const src = typeof link.source === "object" ? (link.source as any).id : link.source;
      const tgt = typeof link.target === "object" ? (link.target as any).id : link.target;
      return src === hoveredNode || tgt === hoveredNode;
    },
    [hoveredNode]
  );

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

  const handleNodeClick = useCallback(
    (node: any) => {
      if (linkMode) {
        const gn: GraphNode = {
          id: node.id,
          name: node.name,
          kind: node.kind,
          path: node.path,
          repoId: node.repoId,
          repoName: node.repoName,
        };

        if (!linkFromNode) {
          setLinkFromNode(gn);
        } else {
          if (linkFromNode.id === node.id) return; // same node
          setLinkToNode(gn);
          setShowLinkModal(true);
        }
      } else {
        setSelectedNode({
          id: node.id,
          name: node.name,
          kind: node.kind,
          path: node.path,
          repoId: node.repoId,
          repoName: node.repoName,
        });
      }
    },
    [linkMode, linkFromNode]
  );

  // Custom node rendering
  const paintNode = useCallback(
    (node: NodeObject<GNode>, ctx: CanvasRenderingContext2D, globalScale: number) => {
      const n = node as NodeObject<GNode>;
      const label = n.name || "";
      const fontSize = Math.max(11 / globalScale, 3);
      const nodeR = Math.max(Math.sqrt(n.val || 4) * 3, 5);
      const color = kindColors[n.kind || ""] || "#545964";
      const highlighted = isHighlighted(n.id!);
      const alpha = highlighted ? (n.isExternal ? 0.6 : 1) : 0.1;

      const x = node.x || 0;
      const y = node.y || 0;

      // Pulsing highlight for link-from node
      if (linkMode && linkFromNode?.id === n.id) {
        ctx.globalAlpha = 0.4;
        ctx.beginPath();
        ctx.arc(x, y, nodeR + 6, 0, 2 * Math.PI);
        ctx.fillStyle = CROSS_REPO_EDGE_COLOR;
        ctx.fill();
      }

      // Multi-repo border ring
      if (isMultiRepo && n.repoId) {
        ctx.globalAlpha = alpha;
        ctx.beginPath();
        ctx.arc(x, y, nodeR + 2, 0, 2 * Math.PI);
        ctx.strokeStyle = repoColorMap.get(n.repoId) || "#545964";
        ctx.lineWidth = 2;
        ctx.stroke();
      }

      // Node circle
      ctx.globalAlpha = alpha;
      ctx.beginPath();
      ctx.arc(x, y, nodeR, 0, 2 * Math.PI);
      ctx.fillStyle = color;
      ctx.fill();

      // Label
      if (globalScale > 0.5 || highlighted) {
        ctx.font = `${fontSize}px -apple-system, BlinkMacSystemFont, sans-serif`;
        ctx.textAlign = "center";
        ctx.textBaseline = "top";
        ctx.fillStyle = "#abb2bf";
        ctx.globalAlpha = alpha * 0.9;
        ctx.fillText(label, x, y + nodeR + 2);

        // Repo badge in multi-repo view
        if (isMultiRepo && n.repoName && globalScale > 0.8) {
          const badgeY = y + nodeR + fontSize + 4;
          ctx.font = `${Math.max(fontSize * 0.75, 2.5)}px -apple-system, BlinkMacSystemFont, sans-serif`;
          ctx.fillStyle = repoColorMap.get(n.repoId || "") || "#787d86";
          ctx.globalAlpha = alpha * 0.7;
          ctx.fillText(n.repoName, x, badgeY);
        }
      }

      ctx.globalAlpha = 1;
    },
    [isHighlighted, linkMode, linkFromNode, isMultiRepo, repoColorMap]
  );

  const paintNodePointerArea = useCallback(
    (node: NodeObject<GNode>, color: string, ctx: CanvasRenderingContext2D) => {
      const nodeR = Math.max(Math.sqrt((node as GNode).val || 4) * 3, 5);
      ctx.beginPath();
      ctx.arc(node.x || 0, node.y || 0, nodeR + 4, 0, 2 * Math.PI);
      ctx.fillStyle = color;
      ctx.fill();
    },
    []
  );

  const getLinkColor = useCallback(
    (link: any) => {
      if (link.crossRepo) {
        return isLinkHighlighted(link) ? CROSS_REPO_EDGE_COLOR : CROSS_REPO_EDGE_COLOR + "20";
      }
      return isLinkHighlighted(link) ? "#545964" : "#54596410";
    },
    [isLinkHighlighted]
  );

  return (
    <div>
      <div className="flex items-center justify-between mb-4">
        <h1 className="text-2xl font-bold text-foreground">Relationship Graph</h1>
        <div className="flex items-center gap-3">
          {/* Link creation toggle */}
          {isMultiRepo && (
            <button
              onClick={() => {
                setLinkMode(!linkMode);
                setLinkFromNode(null);
              }}
              className={`flex items-center gap-1.5 px-2.5 py-1.5 text-xs border rounded-md transition-colors ${
                linkMode
                  ? "border-pink-500/50 text-pink-400 bg-pink-500/10"
                  : "border-edge text-foreground-secondary bg-surface-elevated hover:bg-surface-overlay"
              }`}
            >
              <Link size={12} />
              {linkMode ? "Cancel Link" : "Create Link"}
            </button>
          )}
          <button
            onClick={() => fgRef.current?.zoomToFit(400, 40)}
            className="flex items-center gap-1.5 px-2.5 py-1.5 text-xs border border-edge rounded-md text-foreground-secondary bg-surface-elevated hover:bg-surface-overlay transition-colors"
          >
            <Maximize2 size={12} /> Fit
          </button>
        </div>
      </div>

      {/* Multi-repo selector */}
      <div className="flex flex-wrap items-center gap-2 mb-3">
        <span className="text-xs text-foreground-muted mr-1">Repos:</span>
        {repos.map((r) => {
          const active = selectedRepoIds.has(r.id);
          const color = repoColorMap.get(r.id) || "#545964";
          return (
            <button
              key={r.id}
              onClick={() => toggleRepo(r.id)}
              className={`flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium transition-all ${
                active ? "opacity-100" : "opacity-40"
              }`}
              style={{
                backgroundColor: color + (active ? "26" : "10"),
                color: color,
                border: active ? `1px solid ${color}50` : "1px solid transparent",
              }}
            >
              <div className="w-2 h-2 rounded-full" style={{ backgroundColor: color }} />
              {r.name}
            </button>
          );
        })}
      </div>

      {/* Link mode banner */}
      {linkMode && (
        <div className="flex items-center gap-3 mb-3 px-3 py-2 rounded-md bg-pink-500/10 border border-pink-500/30 text-pink-300 text-sm">
          <Link size={14} />
          {linkFromNode ? (
            <span>
              From: <strong>{linkFromNode.name}</strong> — click a second node to set the target
            </span>
          ) : (
            <span>Click a node to set the link source</span>
          )}
          <button
            onClick={() => {
              setLinkMode(false);
              setLinkFromNode(null);
            }}
            className="ml-auto text-pink-400 hover:text-pink-300"
          >
            <X size={14} />
          </button>
        </div>
      )}

      {/* Filter toolbar */}
      <div className="flex flex-wrap items-center gap-2 mb-3">
        {availableEntityKinds.map((kind) => {
          const active = !hiddenKinds.has(kind);
          return (
            <button
              key={kind}
              onClick={() => toggleKind(kind)}
              className={`flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium transition-all ${
                active ? "opacity-100" : "opacity-30"
              }`}
              style={{
                backgroundColor: (kindColors[kind] || "#545964") + (active ? "26" : "10"),
                color: kindColors[kind] || "#545964",
              }}
            >
              <div className="w-2.5 h-2.5 rounded-sm" style={{ backgroundColor: kindColors[kind] || "#545964" }} />
              {kind}
              {active ? <Eye size={11} /> : <EyeOff size={11} />}
            </button>
          );
        })}

        {availableEntityKinds.length > 0 && availableRelKinds.length > 0 && (
          <div className="w-px h-5 bg-edge mx-1" />
        )}

        {availableRelKinds.map((kind) => {
          const active = !hiddenRelKinds.has(kind);
          return (
            <button
              key={kind}
              onClick={() => toggleRelKind(kind)}
              className={`px-2.5 py-1 rounded-full text-xs font-medium border transition-all ${
                active
                  ? "border-edge text-foreground-secondary bg-surface-overlay"
                  : "border-transparent text-foreground-muted opacity-30"
              }`}
            >
              {relKindLabels[kind] || kind}
            </button>
          );
        })}

        <div className="w-px h-5 bg-edge mx-1" />

        <button
          onClick={() => setShowExtDeps(!showExtDeps)}
          className={`flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium border transition-all ${
            showExtDeps
              ? "border-accent/40 text-accent bg-accent/10"
              : "border-edge text-foreground-muted bg-surface-overlay"
          }`}
        >
          <Package size={11} />
          Ext. Dependencies
          {showExtDeps ? <Eye size={11} /> : <EyeOff size={11} />}
        </button>

        <span className="text-xs text-foreground-muted ml-auto">
          {graphData.nodes.length} nodes, {graphData.links.length} edges
        </span>
      </div>

      <div
        ref={containerRef}
        className="bg-surface-elevated rounded-lg border border-edge overflow-hidden"
        style={{ height: "calc(100vh - 290px)", cursor: linkMode ? "crosshair" : undefined }}
      >
        {loading ? (
          <div className="flex items-center justify-center h-full text-foreground-muted">Loading graph...</div>
        ) : graphData.nodes.length === 0 ? (
          <div className="flex items-center justify-center h-full text-foreground-muted">
            {selectedRepoIds.size > 0 || entityId ? "No relationships found." : "Select repos to view the relationship graph."}
          </div>
        ) : (
          <ForceGraph2D
            ref={fgRef as any}
            graphData={graphData as any}
            width={dimensions.width}
            height={dimensions.height}
            backgroundColor="#24272e"
            nodeCanvasObject={paintNode as any}
            nodeCanvasObjectMode={() => "replace"}
            nodePointerAreaPaint={paintNodePointerArea as any}
            linkColor={getLinkColor}
            linkWidth={(link: any) => {
              if (link.crossRepo) return 2;
              return link.strength === "strong" ? 1.5 : 0.8;
            }}
            linkLineDash={(link: any) => (link.crossRepo ? [5, 3] : null)}
            linkDirectionalArrowLength={4}
            linkDirectionalArrowRelPos={1}
            linkDirectionalArrowColor={(link: any) => {
              if (link.crossRepo) {
                return isLinkHighlighted(link as any) ? CROSS_REPO_EDGE_COLOR : CROSS_REPO_EDGE_COLOR + "20";
              }
              return isLinkHighlighted(link as any) ? "#787d86" : "#54596410";
            }}
            linkCurvature={0.15}
            linkLabel={(link: any) => `${link.kind}${link.crossRepo ? " (cross-repo)" : ""}${link.description ? ": " + link.description : ""}`}
            onNodeClick={handleNodeClick}
            onNodeHover={(node: any) => setHoveredNode(node?.id || null)}
            onBackgroundClick={() => {
              if (linkMode) {
                setLinkFromNode(null);
              } else {
                setSelectedNode(null);
                setHoveredNode(null);
              }
            }}
            cooldownTicks={150}
            enableNodeDrag={true}
            d3AlphaDecay={0.02}
            d3VelocityDecay={0.3}
          />
        )}
      </div>

      {/* Node detail panel */}
      {selectedNode && !linkMode && (
        <div className="fixed right-4 top-20 w-72 bg-surface-elevated rounded-lg border border-edge shadow-lg p-4 z-50">
          <div className="flex items-center justify-between mb-2">
            <span
              className="px-2 py-0.5 rounded text-xs font-medium"
              style={{
                backgroundColor: kindColors[selectedNode.kind] + "26",
                color: kindColors[selectedNode.kind],
              }}
            >
              {selectedNode.kind}
            </span>
            <button onClick={() => setSelectedNode(null)} className="text-foreground-muted hover:text-foreground">
              &times;
            </button>
          </div>
          <h3 className="font-semibold text-foreground">{selectedNode.name}</h3>
          {selectedNode.path && (
            <p className="text-xs text-foreground-secondary mt-1 font-mono">{selectedNode.path}</p>
          )}
          {selectedNode.repoName && (
            <p className="text-xs text-foreground-muted mt-1">Repo: {selectedNode.repoName}</p>
          )}
          <div className="flex gap-2 mt-3">
            <a href={`/entities/${selectedNode.id}`} className="text-xs text-accent hover:underline">
              View Details
            </a>
            <button
              onClick={() => {
                setSearchParams({ entity: selectedNode.id });
                setSelectedNode(null);
              }}
              className="text-xs text-accent hover:underline"
            >
              Focus Graph
            </button>
          </div>
        </div>
      )}

      {/* Link creation modal */}
      {showLinkModal && linkFromNode && linkToNode && (
        <CreateLinkModal
          fromNode={linkFromNode}
          toNode={linkToNode}
          onClose={() => {
            setShowLinkModal(false);
            setLinkFromNode(null);
            setLinkToNode(null);
          }}
          onCreated={() => {
            setShowLinkModal(false);
            setLinkMode(false);
            setLinkFromNode(null);
            setLinkToNode(null);
            loadGraph();
          }}
        />
      )}
    </div>
  );
}
