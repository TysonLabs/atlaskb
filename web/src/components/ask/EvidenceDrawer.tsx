import { useState } from "react";
import { ChevronDown, FileText } from "lucide-react";
import type { SearchResult } from "../../types";

interface EvidenceDrawerProps {
  evidence: SearchResult[];
}

export function EvidenceDrawer({ evidence }: EvidenceDrawerProps) {
  const [isOpen, setIsOpen] = useState(false);

  if (!evidence || evidence.length === 0) return null;

  return (
    <div className="mt-2">
      <button
        onClick={() => setIsOpen(!isOpen)}
        className="flex items-center gap-1.5 text-xs text-foreground-muted hover:text-foreground transition-colors"
      >
        <FileText size={12} />
        <span>{evidence.length} source{evidence.length !== 1 ? "s" : ""}</span>
        <ChevronDown
          size={12}
          className={`transition-transform duration-200 ${isOpen ? "rotate-180" : ""}`}
        />
      </button>

      <div
        className={`overflow-hidden transition-all duration-300 ease-in-out ${
          isOpen ? "max-h-[400px] opacity-100 mt-2" : "max-h-0 opacity-0"
        }`}
      >
        <div className="space-y-2 overflow-y-auto max-h-[380px]">
          {evidence.slice(0, 20).map((result, i) => (
            <div
              key={i}
              className="bg-surface rounded-lg border border-edge p-3"
            >
              <p className="text-xs text-foreground leading-relaxed">{result.fact.claim}</p>
              <div className="flex items-center gap-2 mt-2">
                <span className="text-[10px] px-1.5 py-0.5 bg-syn-blue/15 text-syn-blue rounded">
                  {result.entity.name}
                </span>
                <span className="text-[10px] text-foreground-muted">{result.source}</span>
                <span className="text-[10px] text-foreground-muted">
                  {(result.score * 100).toFixed(0)}%
                </span>
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
