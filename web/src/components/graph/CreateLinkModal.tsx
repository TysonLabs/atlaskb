import { useState } from "react";
import { api } from "../../api/client";
import type { GraphNode } from "../../types";
import { X } from "lucide-react";

const kindOptions = [
  "depends_on",
  "calls",
  "implements",
  "extends",
  "consumes",
  "emits",
  "reads",
  "writes",
];

const strengthOptions = ["strong", "moderate", "weak"];

interface Props {
  fromNode: GraphNode;
  toNode: GraphNode;
  onClose: () => void;
  onCreated: () => void;
}

export function CreateLinkModal({ fromNode, toNode, onClose, onCreated }: Props) {
  const [kind, setKind] = useState("depends_on");
  const [strength, setStrength] = useState("moderate");
  const [description, setDescription] = useState("");
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const sameRepo = fromNode.repoId && toNode.repoId && fromNode.repoId === toNode.repoId;

  const handleCreate = async () => {
    setCreating(true);
    setError(null);
    try {
      await api.createCrossRepoLink({
        from_entity_id: fromNode.id,
        to_entity_id: toNode.id,
        kind,
        strength,
        description: description || undefined,
      });
      onCreated();
    } catch (err: any) {
      setError(err.message || "Failed to create link");
    } finally {
      setCreating(false);
    }
  };

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div className="bg-surface-elevated rounded-lg border border-edge shadow-xl w-full max-w-md p-6">
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-lg font-semibold text-foreground">Create Cross-Repo Link</h2>
          <button onClick={onClose} className="text-foreground-muted hover:text-foreground">
            <X size={18} />
          </button>
        </div>

        {sameRepo && (
          <div className="mb-4 p-3 rounded-md bg-yellow-500/10 border border-yellow-500/30 text-yellow-400 text-sm">
            Both entities belong to the same repo. Cross-repo links are typically between different repos.
          </div>
        )}

        <div className="space-y-4">
          <div>
            <label className="block text-xs font-medium text-foreground-secondary mb-1">From</label>
            <div className="px-3 py-2 bg-surface-overlay rounded-md border border-edge text-sm text-foreground">
              {fromNode.name}
              {fromNode.repoName && (
                <span className="text-foreground-muted ml-1">({fromNode.repoName})</span>
              )}
            </div>
          </div>

          <div>
            <label className="block text-xs font-medium text-foreground-secondary mb-1">To</label>
            <div className="px-3 py-2 bg-surface-overlay rounded-md border border-edge text-sm text-foreground">
              {toNode.name}
              {toNode.repoName && (
                <span className="text-foreground-muted ml-1">({toNode.repoName})</span>
              )}
            </div>
          </div>

          <div>
            <label className="block text-xs font-medium text-foreground-secondary mb-1">Kind</label>
            <select
              value={kind}
              onChange={(e) => setKind(e.target.value)}
              className="w-full px-3 py-2 bg-surface-overlay rounded-md border border-edge text-sm text-foreground"
            >
              {kindOptions.map((k) => (
                <option key={k} value={k}>
                  {k.replace(/_/g, " ")}
                </option>
              ))}
            </select>
          </div>

          <div>
            <label className="block text-xs font-medium text-foreground-secondary mb-1">Strength</label>
            <div className="flex gap-2">
              {strengthOptions.map((s) => (
                <button
                  key={s}
                  onClick={() => setStrength(s)}
                  className={`flex-1 px-3 py-1.5 rounded-md text-xs font-medium border transition-colors ${
                    strength === s
                      ? "border-accent text-accent bg-accent/10"
                      : "border-edge text-foreground-secondary bg-surface-overlay hover:bg-surface-overlay/80"
                  }`}
                >
                  {s}
                </button>
              ))}
            </div>
          </div>

          <div>
            <label className="block text-xs font-medium text-foreground-secondary mb-1">
              Description <span className="text-foreground-muted">(optional)</span>
            </label>
            <input
              type="text"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="e.g., Consumes user events from..."
              className="w-full px-3 py-2 bg-surface-overlay rounded-md border border-edge text-sm text-foreground placeholder:text-foreground-muted"
            />
          </div>

          {error && (
            <div className="p-3 rounded-md bg-red-500/10 border border-red-500/30 text-red-400 text-sm">
              {error}
            </div>
          )}

          <div className="flex gap-3 pt-2">
            <button
              onClick={onClose}
              className="flex-1 px-4 py-2 rounded-md border border-edge text-sm text-foreground-secondary hover:bg-surface-overlay transition-colors"
            >
              Cancel
            </button>
            <button
              onClick={handleCreate}
              disabled={creating}
              className="flex-1 px-4 py-2 rounded-md bg-accent text-white text-sm font-medium hover:bg-accent/90 transition-colors disabled:opacity-50"
            >
              {creating ? "Creating..." : "Create Link"}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
