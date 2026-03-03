import { useEffect, useState, useCallback, useRef } from "react";
import mermaid from "mermaid";
import type { ExecutionFlow } from "../../types";
import { Clipboard, X, ZoomIn, ZoomOut, RotateCcw } from "lucide-react";

mermaid.initialize({
  startOnLoad: false,
  theme: "dark",
  themeVariables: {
    primaryColor: "#3b4252",
    primaryTextColor: "#d8dee9",
    primaryBorderColor: "#4c566a",
    lineColor: "#88c0d0",
    secondaryColor: "#434c5e",
    tertiaryColor: "#2e3440",
    background: "#2e3440",
    mainBkg: "#3b4252",
    nodeBorder: "#4c566a",
    clusterBkg: "#3b4252",
    titleColor: "#eceff4",
    edgeLabelBackground: "#2e3440",
  },
});

interface Props {
  flow: ExecutionFlow;
  onClose: () => void;
  onEntityClick: (id: string) => void;
}

function buildMermaidCode(flow: ExecutionFlow): string {
  const lines: string[] = ["graph TD"];
  const names = flow.step_names;

  for (let i = 0; i < names.length; i++) {
    const label = names[i].replace(/"/g, "#quot;");
    if (i === 0) {
      lines.push(`    step${i}["${label}"]:::entry`);
    } else {
      lines.push(`    step${i}["${label}"]`);
    }
  }

  for (let i = 0; i < names.length - 1; i++) {
    lines.push(`    step${i} --> step${i + 1}`);
  }

  lines.push("    classDef entry fill:#98c379,color:#282c34,stroke:#98c379");

  return lines.join("\n");
}

export function FlowModal({ flow, onClose, onEntityClick: _onEntityClick }: Props) {
  const [svgHtml, setSvgHtml] = useState<string>("");
  const [zoom, setZoom] = useState(1);
  const [copied, setCopied] = useState(false);
  const renderIdRef = useRef(0);

  const mermaidCode = buildMermaidCode(flow);

  useEffect(() => {
    const currentId = ++renderIdRef.current;
    const renderDiagram = async () => {
      try {
        const { svg } = await mermaid.render(`flow-diagram-${flow.id.replace(/[^a-zA-Z0-9]/g, "")}`, mermaidCode);
        if (currentId === renderIdRef.current) {
          setSvgHtml(svg);
        }
      } catch (err) {
        console.error("Mermaid render error:", err);
        if (currentId === renderIdRef.current) {
          setSvgHtml(`<p style="color: #bf616a;">Failed to render diagram</p>`);
        }
      }
    };
    renderDiagram();
  }, [flow.id, mermaidCode]);

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    },
    [onClose],
  );

  useEffect(() => {
    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [handleKeyDown]);

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(mermaidCode);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // clipboard may not be available
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm"
      onClick={onClose}
    >
      <div
        className="max-w-4xl w-full max-h-[85vh] bg-surface-elevated border border-edge rounded-2xl shadow-2xl flex flex-col mx-4"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-edge">
          <div className="flex items-center gap-3 min-w-0">
            <h2 className="font-semibold text-foreground truncate">{flow.label}</h2>
            <span className="text-xs text-foreground-muted shrink-0">
              {flow.step_names.length} steps
            </span>
          </div>
          <div className="flex items-center gap-2 shrink-0">
            <button
              onClick={handleCopy}
              className="flex items-center gap-1.5 px-2.5 py-1.5 text-xs rounded-md border border-edge text-foreground-secondary hover:text-foreground hover:bg-surface-overlay transition-colors"
              title="Copy Mermaid source"
            >
              <Clipboard size={14} />
              {copied ? "Copied" : "Copy Mermaid"}
            </button>
            <button
              onClick={onClose}
              className="p-1.5 rounded-md text-foreground-muted hover:text-foreground hover:bg-surface-overlay transition-colors"
              title="Close"
            >
              <X size={18} />
            </button>
          </div>
        </div>

        {/* Zoom controls */}
        <div className="flex items-center gap-2 px-6 py-2 border-b border-edge">
          <button
            onClick={() => setZoom((z) => Math.max(0.25, z - 0.25))}
            className="p-1 rounded-md text-foreground-muted hover:text-foreground hover:bg-surface-overlay transition-colors"
            title="Zoom out"
          >
            <ZoomOut size={16} />
          </button>
          <span className="text-xs text-foreground-secondary w-12 text-center">
            {Math.round(zoom * 100)}%
          </span>
          <button
            onClick={() => setZoom((z) => Math.min(3, z + 0.25))}
            className="p-1 rounded-md text-foreground-muted hover:text-foreground hover:bg-surface-overlay transition-colors"
            title="Zoom in"
          >
            <ZoomIn size={16} />
          </button>
          <button
            onClick={() => setZoom(1)}
            className="p-1 rounded-md text-foreground-muted hover:text-foreground hover:bg-surface-overlay transition-colors"
            title="Reset zoom"
          >
            <RotateCcw size={14} />
          </button>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-auto p-6">
          <div
            style={{ transform: `scale(${zoom})`, transformOrigin: "top left" }}
            dangerouslySetInnerHTML={{ __html: svgHtml }}
          />
        </div>
      </div>
    </div>
  );
}
