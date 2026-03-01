import { useEffect, useState } from "react";
import { useParams, Link } from "react-router-dom";
import { api } from "../../api/client";
import type { EntityDetail as EntityDetailType, Decision } from "../../types";
import { ArrowLeft, Network } from "lucide-react";

export function EntityDetailPage() {
  const { id } = useParams<{ id: string }>();
  const [entity, setEntity] = useState<EntityDetailType | null>(null);
  const [decisions, setDecisions] = useState<Decision[]>([]);
  const [tab, setTab] = useState<"facts" | "relationships" | "decisions">("facts");

  useEffect(() => {
    if (!id) return;
    api.getEntity(id).then(setEntity).catch(console.error);
    api.getEntityDecisions(id).then(setDecisions).catch(console.error);
  }, [id]);

  if (!entity) return <p className="text-foreground-secondary">Loading...</p>;

  return (
    <div>
      <Link to="/entities" className="text-sm text-accent hover:underline flex items-center gap-1 mb-4">
        <ArrowLeft size={14} /> Back to Entities
      </Link>

      <div className="flex items-start justify-between mb-6">
        <div>
          <div className="flex items-center gap-2 mb-1">
            <span className={`px-2 py-0.5 rounded text-xs font-medium ${kindColor(entity.kind)}`}>{entity.kind}</span>
            <h1 className="text-2xl font-bold text-foreground">{entity.name}</h1>
          </div>
          <p className="text-sm text-foreground-secondary">{entity.qualified_name}</p>
          {entity.path && <p className="text-xs text-foreground-muted mt-1 font-mono">{entity.path}</p>}
        </div>
        <Link
          to={`/graph?entity=${id}`}
          className="flex items-center gap-1 text-sm px-3 py-1.5 bg-accent text-surface font-medium rounded-md hover:bg-accent-hover transition-colors"
        >
          <Network size={14} /> View Graph
        </Link>
      </div>

      {entity.summary && (
        <p className="text-sm text-foreground bg-surface-elevated rounded-lg p-3 mb-4 border border-edge">{entity.summary}</p>
      )}

      {entity.capabilities && entity.capabilities.length > 0 && (
        <div className="mb-4">
          <h3 className="text-xs font-semibold text-foreground-secondary mb-1">Capabilities</h3>
          <div className="flex flex-wrap gap-1.5">
            {entity.capabilities.map((c, i) => (
              <span key={i} className="px-2 py-0.5 bg-syn-blue/15 text-syn-blue rounded text-xs">{c}</span>
            ))}
          </div>
        </div>
      )}

      {/* Tabs */}
      <div className="border-b border-edge mb-4">
        <div className="flex gap-4">
          {([
            ["facts", `Facts (${entity.facts.length})`],
            ["relationships", `Relationships (${entity.relationships.length})`],
            ["decisions", `Decisions (${decisions.length})`],
          ] as const).map(([key, label]) => (
            <button
              key={key}
              onClick={() => setTab(key as typeof tab)}
              className={`pb-2 text-sm font-medium border-b-2 transition-colors ${
                tab === key ? "border-accent text-accent" : "border-transparent text-foreground-secondary hover:text-foreground"
              }`}
            >
              {label}
            </button>
          ))}
        </div>
      </div>

      {tab === "facts" && (
        <div className="space-y-2">
          {entity.facts.length === 0 ? (
            <p className="text-sm text-foreground-secondary">No facts.</p>
          ) : (
            entity.facts.map((fact) => (
              <div key={fact.id} className="bg-surface-elevated rounded-lg border border-edge p-3">
                <p className="text-sm text-foreground">{fact.claim}</p>
                <div className="flex gap-2 mt-2">
                  <span className="text-[10px] px-1.5 py-0.5 rounded bg-surface-overlay text-foreground-secondary">{fact.dimension}</span>
                  <span className="text-[10px] px-1.5 py-0.5 rounded bg-surface-overlay text-foreground-secondary">{fact.category}</span>
                  <span className={`text-[10px] px-1.5 py-0.5 rounded ${confidenceColor(fact.confidence)}`}>{fact.confidence}</span>
                </div>
              </div>
            ))
          )}
        </div>
      )}

      {tab === "relationships" && (
        <div className="bg-surface-elevated rounded-lg border border-edge divide-y divide-edge">
          {entity.relationships.length === 0 ? (
            <p className="p-4 text-sm text-foreground-secondary">No relationships.</p>
          ) : (
            entity.relationships.map((rel) => (
              <div key={rel.id} className="px-4 py-3 text-sm">
                <div className="flex items-center gap-2">
                  <Link
                    to={`/entities/${rel.from_entity_id}`}
                    className="text-accent hover:underline font-medium"
                  >
                    {rel.from_entity_name || rel.from_entity_id.slice(0, 8)}
                  </Link>
                  <span className="px-2 py-0.5 bg-surface-overlay rounded text-xs font-mono text-foreground-secondary">{rel.kind}</span>
                  <Link
                    to={`/entities/${rel.to_entity_id}`}
                    className="text-accent hover:underline font-medium"
                  >
                    {rel.to_entity_name || rel.to_entity_id.slice(0, 8)}
                  </Link>
                </div>
                {rel.description && <p className="text-xs text-foreground-secondary mt-1">{rel.description}</p>}
              </div>
            ))
          )}
        </div>
      )}

      {tab === "decisions" && (
        <div className="space-y-3">
          {decisions.length === 0 ? (
            <p className="text-sm text-foreground-secondary">No decisions linked to this entity.</p>
          ) : (
            decisions.map((d) => (
              <div key={d.id} className="bg-surface-elevated rounded-lg border border-edge p-4">
                <h3 className="font-medium text-foreground">{d.summary}</h3>
                <p className="text-sm text-foreground-secondary mt-1">{d.description}</p>
              </div>
            ))
          )}
        </div>
      )}
    </div>
  );
}

function kindColor(kind: string): string {
  const map: Record<string, string> = {
    module: "bg-syn-blue/15 text-syn-blue",
    service: "bg-syn-green/15 text-syn-green",
    function: "bg-syn-magenta/15 text-syn-magenta",
    type: "bg-syn-orange/15 text-syn-orange",
    endpoint: "bg-syn-red/15 text-syn-red",
    concept: "bg-surface-overlay text-foreground-secondary",
    config: "bg-syn-yellow/15 text-syn-yellow",
  };
  return map[kind] || "bg-surface-overlay text-foreground-secondary";
}

function confidenceColor(c: string): string {
  if (c === "high") return "bg-syn-green/15 text-syn-green";
  if (c === "medium") return "bg-syn-yellow/15 text-syn-yellow";
  return "bg-syn-red/15 text-syn-red";
}
