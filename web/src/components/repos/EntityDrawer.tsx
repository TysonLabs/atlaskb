import { useEffect, useState } from "react";
import { api } from "../../api/client";
import type { EntityDetail, Fact, Relationship } from "../../types";
import { X, Loader2, ChevronRight, ChevronLeft, Code } from "lucide-react";
import { Prism as SyntaxHighlighter } from "react-syntax-highlighter";
import { oneDark } from "react-syntax-highlighter/dist/esm/styles/prism";

const kindColor: Record<string, string> = {
  module: "bg-syn-blue/15 text-syn-blue",
  service: "bg-syn-green/15 text-syn-green",
  function: "bg-syn-magenta/15 text-syn-magenta",
  type: "bg-syn-orange/15 text-syn-orange",
  endpoint: "bg-syn-red/15 text-syn-red",
  concept: "bg-surface-overlay text-foreground-secondary",
  config: "bg-syn-yellow/15 text-syn-yellow",
  cluster: "bg-syn-cyan/15 text-syn-cyan",
};

const extToLang: Record<string, string> = {
  go: "go",
  ts: "typescript",
  tsx: "typescript",
  js: "javascript",
  jsx: "javascript",
  py: "python",
  rs: "rust",
  rb: "ruby",
  java: "java",
  kt: "kotlin",
  swift: "swift",
  c: "c",
  cpp: "cpp",
  h: "c",
  hpp: "cpp",
  cs: "csharp",
  css: "css",
  scss: "scss",
  html: "html",
  json: "json",
  yaml: "yaml",
  yml: "yaml",
  toml: "toml",
  sql: "sql",
  sh: "bash",
  bash: "bash",
  zsh: "bash",
  md: "markdown",
  dockerfile: "docker",
};

function getLang(path: string): string {
  const name = path.split("/").pop() || "";
  if (name.toLowerCase() === "dockerfile") return "docker";
  const ext = name.split(".").pop()?.toLowerCase() || "";
  return extToLang[ext] || "text";
}

const STORAGE_KEY = "atlaskb.drawer.width";

interface Props {
  entityId: string | null;
  onClose: () => void;
  onEntityClick?: (id: string) => void;
}

export function EntityDrawer({ entityId, onClose, onEntityClick }: Props) {
  const [entity, setEntity] = useState<EntityDetail | null>(null);
  const [facts, setFacts] = useState<Fact[]>([]);
  const [relationships, setRelationships] = useState<Relationship[]>([]);
  const [loading, setLoading] = useState(false);
  const [collapsed, setCollapsed] = useState<boolean>(false);

  // Resizable width
  const [width, setWidth] = useState(() => {
    const saved = localStorage.getItem(STORAGE_KEY);
    return saved ? parseInt(saved) : 384;
  });
  const [dragging, setDragging] = useState(false);

  useEffect(() => {
    if (!dragging) return;
    const onMove = (e: MouseEvent) => {
      const newWidth = Math.min(800, Math.max(320, window.innerWidth - e.clientX));
      setWidth(newWidth);
    };
    const onUp = () => {
      setDragging(false);
      localStorage.setItem(STORAGE_KEY, String(width));
    };
    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
    return () => {
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", onUp);
    };
  }, [dragging, width]);

  // Source code viewer
  const [sourceContent, setSourceContent] = useState<string | null>(null);
  const [sourceLoading, setSourceLoading] = useState(false);
  const [sourceError, setSourceError] = useState<string | null>(null);
  const [sourceExpanded, setSourceExpanded] = useState(false);

  useEffect(() => {
    // Reset source state when entity changes
    setSourceContent(null);
    setSourceError(null);
    setSourceLoading(false);
    setSourceExpanded(false);
  }, [entityId]);

  useEffect(() => {
    if (!sourceExpanded || !entity?.path || !entity?.repo_id) return;
    if (sourceContent !== null || sourceLoading) return;

    setSourceLoading(true);
    api
      .getFileContent(entity.repo_id, entity.path)
      .then((res) => setSourceContent(res.content))
      .catch((err) => setSourceError(err.message || "Failed to load file"))
      .finally(() => setSourceLoading(false));
  }, [sourceExpanded, entity, sourceContent, sourceLoading]);

  useEffect(() => {
    if (!entityId) {
      setEntity(null);
      return;
    }
    let cancelled = false;
    setLoading(true);
    setEntity(null);
    setFacts([]);
    setRelationships([]);
    Promise.all([
      api.getEntity(entityId),
      api.getEntityFacts(entityId),
      api.getEntityRelationships(entityId),
    ])
      .then(([e, f, r]) => {
        if (cancelled) return;
        setEntity(e);
        setFacts(f);
        setRelationships(r);
      })
      .catch(console.error)
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [entityId]);

  if (!entityId) return null;

  const factsByCategory = facts.reduce<Record<string, Fact[]>>((acc, f) => {
    const cat = f.category || "general";
    (acc[cat] ||= []).push(f);
    return acc;
  }, {});

  if (collapsed) {
    return (
      <div className="fixed top-0 right-0 h-full w-12 bg-surface-elevated border-l border-edge shadow-2xl z-50 flex flex-col items-center animate-in slide-in-from-right">
        <button
          onClick={() => setCollapsed(false)}
          className="p-3 text-foreground-muted hover:text-foreground"
          title="Expand drawer"
        >
          <ChevronLeft size={16} />
        </button>
        <div className="flex-1 flex flex-col items-center justify-center overflow-hidden">
          <span
            className={`inline-block px-1.5 py-0.5 rounded text-xs font-medium ${kindColor[entity?.kind || ""] || kindColor.concept}`}
            style={{ writingMode: "vertical-rl", textOrientation: "mixed" }}
          >
            {entity?.kind || ""}
          </span>
          <span
            className="text-xs text-foreground font-medium mt-2 max-h-48 overflow-hidden"
            style={{ writingMode: "vertical-rl", textOrientation: "mixed" }}
          >
            {entity?.name || "Entity"}
          </span>
        </div>
      </div>
    );
  }

  const lang = entity?.path ? getLang(entity.path) : "text";

  return (
    <div
      className="fixed top-0 right-0 h-full bg-surface-elevated border-l border-edge shadow-2xl z-50 flex flex-col animate-in slide-in-from-right"
      style={{ width }}
    >
      {/* Drag handle */}
      <div
        onMouseDown={() => setDragging(true)}
        className="absolute left-0 top-0 h-full w-1 cursor-col-resize hover:bg-accent/30 transition-colors z-10"
      />

      {/* Header */}
      <div className="flex items-center justify-between p-4 border-b border-edge">
        <div className="flex items-center gap-2 min-w-0">
          <button
            onClick={() => setCollapsed(true)}
            className="text-foreground-muted hover:text-foreground shrink-0"
            title="Collapse drawer"
          >
            <ChevronRight size={16} />
          </button>
          <h3 className="text-sm font-semibold text-foreground truncate">
            {loading ? "Loading..." : entity?.name || "Entity"}
          </h3>
        </div>
        <div className="flex items-center gap-1 shrink-0">
          <button onClick={onClose} className="text-foreground-muted hover:text-foreground">
            <X size={16} />
          </button>
        </div>
      </div>

      {/* Body */}
      <div className="flex-1 overflow-y-auto p-4 space-y-4">
        {loading ? (
          <div className="flex items-center justify-center py-12">
            <Loader2 size={24} className="animate-spin text-foreground-muted" />
          </div>
        ) : entity ? (
          <>
            {/* Kind badge + path */}
            <div className="space-y-2">
              <span className={`inline-block px-2 py-0.5 rounded-full text-xs font-medium ${kindColor[entity.kind] || kindColor.concept}`}>
                {entity.kind}
              </span>
              {entity.path && (
                <p className="text-xs text-foreground-muted font-mono">{entity.path}</p>
              )}
              <p className="text-xs text-foreground-secondary font-mono">{entity.qualified_name}</p>
            </div>

            {/* Summary */}
            {entity.summary && (
              <div>
                <h4 className="text-xs font-semibold text-foreground-secondary mb-1">Summary</h4>
                <p className="text-sm text-foreground">{entity.summary}</p>
              </div>
            )}

            {/* Source Code */}
            {entity.path && (
              <details
                className="group"
                onToggle={(e) => setSourceExpanded((e.target as HTMLDetailsElement).open)}
              >
                <summary className="flex items-center gap-1.5 text-xs font-semibold text-foreground-secondary cursor-pointer">
                  <Code size={12} /> Source Code
                  <span className="text-foreground-muted font-normal">{entity.path}</span>
                </summary>
                <div className="mt-2 rounded-lg overflow-hidden border border-edge text-xs">
                  {sourceLoading ? (
                    <p className="p-2 text-foreground-muted">Loading...</p>
                  ) : sourceContent ? (
                    <SyntaxHighlighter
                      language={lang}
                      style={oneDark}
                      showLineNumbers
                      customStyle={{ margin: 0, fontSize: "11px", maxHeight: "400px" }}
                    >
                      {sourceContent}
                    </SyntaxHighlighter>
                  ) : sourceError ? (
                    <p className="text-syn-red p-2">{sourceError}</p>
                  ) : null}
                </div>
              </details>
            )}

            {/* Capabilities */}
            {entity.capabilities && entity.capabilities.length > 0 && (
              <div>
                <h4 className="text-xs font-semibold text-foreground-secondary mb-1">Capabilities</h4>
                <ul className="space-y-0.5">
                  {entity.capabilities.map((c, i) => (
                    <li key={i} className="text-sm text-foreground-secondary flex items-start gap-1.5">
                      <span className="text-accent mt-0.5">-</span> {c}
                    </li>
                  ))}
                </ul>
              </div>
            )}

            {/* Assumptions */}
            {entity.assumptions && entity.assumptions.length > 0 && (
              <div>
                <h4 className="text-xs font-semibold text-foreground-secondary mb-1">Assumptions</h4>
                <ul className="space-y-0.5">
                  {entity.assumptions.map((a, i) => (
                    <li key={i} className="text-sm text-foreground-secondary flex items-start gap-1.5">
                      <span className="text-syn-yellow mt-0.5">-</span> {a}
                    </li>
                  ))}
                </ul>
              </div>
            )}

            {/* Facts by category */}
            {Object.keys(factsByCategory).length > 0 && (
              <div>
                <h4 className="text-xs font-semibold text-foreground-secondary mb-2">Facts</h4>
                {Object.entries(factsByCategory).map(([cat, catFacts]) => (
                  <div key={cat} className="mb-3">
                    <p className="text-xs font-medium text-foreground-muted uppercase tracking-wide mb-1">{cat}</p>
                    <div className="space-y-1">
                      {catFacts.map((f) => (
                        <div key={f.id} className={`text-sm text-foreground-secondary bg-surface rounded px-2 py-1.5${f.superseded_by ? " opacity-60" : ""}`}>
                          <span className={f.superseded_by ? "line-through" : ""}>{f.claim}</span>
                          <span className="text-xs text-foreground-muted ml-1">({f.confidence})</span>
                          {f.superseded_by && (
                            <span className="text-[10px] font-medium text-syn-yellow bg-syn-yellow/15 px-1 py-0.5 rounded ml-1.5">outdated</span>
                          )}
                          {f.provenance && f.provenance.length > 0 && (
                            <div className="flex flex-wrap gap-1.5 mt-1">
                              {f.provenance.map((p, i) => (
                                <span key={i} className="inline-flex items-center gap-0.5">
                                  <span className="bg-surface-overlay text-foreground-muted px-1 py-0.5 rounded text-[10px] font-medium">{p.source_type}</span>
                                  {p.url ? (
                                    <a href={p.url} target="_blank" rel="noopener noreferrer" className="text-[10px] text-accent hover:underline">{p.ref}</a>
                                  ) : (
                                    <span className="text-[10px] text-foreground-muted">{p.ref}</span>
                                  )}
                                </span>
                              ))}
                            </div>
                          )}
                        </div>
                      ))}
                    </div>
                  </div>
                ))}
              </div>
            )}

            {/* Relationships */}
            {relationships.length > 0 && (
              <div>
                <h4 className="text-xs font-semibold text-foreground-secondary mb-2">Relationships ({relationships.length})</h4>
                <div className="space-y-1">
                  {relationships.map((r) => {
                    const isOutgoing = r.from_entity_id === entity.id;
                    const otherName = isOutgoing ? r.to_entity_name : r.from_entity_name;
                    const otherId = isOutgoing ? r.to_entity_id : r.from_entity_id;
                    const strengthDot = r.strength === "strong"
                      ? "bg-syn-green"
                      : r.strength === "moderate"
                        ? "bg-syn-yellow"
                        : "bg-foreground-muted";
                    return (
                      <div key={r.id} className="space-y-0.5">
                        <div className="flex items-center gap-1.5 text-sm">
                          <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${strengthDot}`} title={`Strength: ${r.strength}`} />
                          <span className="text-foreground-muted text-xs w-20 text-right shrink-0">
                            {isOutgoing ? r.kind : `${r.kind} by`}
                          </span>
                          <span className="text-foreground-muted">{isOutgoing ? "\u2192" : "\u2190"}</span>
                          <button
                            onClick={() => onEntityClick?.(otherId)}
                            className="text-accent hover:underline truncate text-left"
                          >
                            {otherName || otherId.slice(0, 8)}
                          </button>
                        </div>
                        {r.description && (
                          <p className="text-xs text-foreground-muted ml-5 pl-20">{r.description}</p>
                        )}
                      </div>
                    );
                  })}
                </div>
              </div>
            )}
          </>
        ) : (
          <p className="text-sm text-foreground-secondary">Entity not found.</p>
        )}
      </div>
    </div>
  );
}
