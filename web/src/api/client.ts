import type {
  Stats,
  IndexingRun,
  Repo,
  RepoListItem,
  RepoDetail,
  EntitySearchResult,
  EntityDetail,
  Fact,
  Relationship,
  Decision,
  SearchResult,
  ChatSession,
  ChatSessionSummary,
  BatchStatus,
  IndexingJobSummary,
  FunctionalCluster,
  ExecutionFlow,
} from "../types";

const BASE = "/api";

async function get<T>(path: string): Promise<T> {
  const res = await fetch(`${BASE}${path}`);
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw new Error(body.error || `HTTP ${res.status}`);
  }
  return res.json();
}

async function post<T>(path: string, body: unknown): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new Error(data.error || `HTTP ${res.status}`);
  }
  return res.json();
}

async function put<T>(path: string, body: unknown): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new Error(data.error || `HTTP ${res.status}`);
  }
  return res.json();
}

async function del<T>(path: string): Promise<T> {
  const res = await fetch(`${BASE}${path}`, { method: "DELETE" });
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new Error(data.error || `HTTP ${res.status}`);
  }
  return res.json();
}

export const api = {
  getStats: () => get<Stats>("/stats"),
  getRecentRuns: () => get<IndexingRun[]>("/stats/recent-runs"),

  listRepos: () => get<RepoListItem[]>("/repos"),
  getRepo: (id: string) => get<RepoDetail>(`/repos/${id}`),
  createRepo: (data: { name: string; local_path: string; default_branch?: string; exclude_dirs?: string[] }) =>
    post<Repo>("/repos", data),
  updateRepo: (id: string, data: { name?: string; exclude_dirs?: string[] }) =>
    put<Repo & { reindex_required: boolean }>(`/repos/${id}`, data),
  deleteRepo: (id: string) => del<{ status: string }>(`/repos/${id}`),
  reindexRepo: (id: string, opts?: { force?: boolean; phases?: string[]; concurrency?: number }) =>
    post<{ status: string; message: string }>(`/repos/${id}/reindex`, {
      force: opts?.force || false,
      ...(opts?.phases?.length ? { phases: opts.phases } : {}),
      ...(opts?.concurrency ? { concurrency: opts.concurrency } : {}),
    }),
  getReindexStatus: (id: string) =>
    get<{ status: string; logs: string[] }>(`/repos/${id}/reindex/status`),
  getRepoIndexingRuns: (id: string) => get<IndexingRun[]>(`/repos/${id}/indexing-runs`),
  getRepoDecisions: (id: string) => get<Decision[]>(`/repos/${id}/decisions`),
  getRepoClusters: (id: string) => get<FunctionalCluster[]>(`/repos/${id}/clusters`),
  getRepoFlows: (id: string) => get<ExecutionFlow[]>(`/repos/${id}/flows`),

  listEntities: (params: {
    repo_id?: string;
    kind?: string;
    q?: string;
    limit?: number;
    offset?: number;
  }) => {
    const sp = new URLSearchParams();
    if (params.repo_id) sp.set("repo_id", params.repo_id);
    if (params.kind) sp.set("kind", params.kind);
    if (params.q) sp.set("q", params.q);
    if (params.limit) sp.set("limit", String(params.limit));
    if (params.offset) sp.set("offset", String(params.offset));
    return get<EntitySearchResult>(`/entities?${sp}`);
  },

  getEntity: (id: string) => get<EntityDetail>(`/entities/${id}`),
  getEntityFacts: (id: string) => get<Fact[]>(`/entities/${id}/facts`),
  getEntityRelationships: (id: string) => get<Relationship[]>(`/entities/${id}/relationships`),
  getEntityDecisions: (id: string) => get<Decision[]>(`/entities/${id}/decisions`),

  search: (q: string, repoId?: string) => {
    const sp = new URLSearchParams({ q });
    if (repoId) sp.set("repo_id", repoId);
    return get<SearchResult[]>(`/search?${sp}`);
  },

  ask: async function* (question: string, repoId?: string, topK?: number) {
    const res = await fetch(`${BASE}/ask`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ question, repo_id: repoId || undefined, top_k: topK || 40 }),
    });

    if (!res.ok) {
      const body = await res.json().catch(() => ({}));
      throw new Error(body.error || `HTTP ${res.status}`);
    }

    const reader = res.body!.getReader();
    const decoder = new TextDecoder();
    let buffer = "";

    while (true) {
      const { done, value } = await reader.read();
      if (done) break;

      buffer += decoder.decode(value, { stream: true });
      const lines = buffer.split("\n");
      buffer = lines.pop() || "";

      let eventType = "";
      for (const line of lines) {
        if (line.startsWith("event: ")) {
          eventType = line.slice(7);
        } else if (line.startsWith("data: ")) {
          const data = line.slice(6);
          yield { event: eventType, data };
          eventType = "";
        }
      }
    }
  },

  // Batch / indexing
  batchReindexAll: (force?: boolean) =>
    post<{ status: string; batch_id: string; message: string }>("/indexing/batch", { all: true, force: force || false }),
  batchReindex: (repoIds: string[], force?: boolean) =>
    post<{ status: string; batch_id: string; message: string }>("/indexing/batch", { repo_ids: repoIds, force: force || false }),
  getBatchStatus: () => get<BatchStatus>("/indexing/batch/status"),
  cancelBatch: () => post<{ status: string }>("/indexing/batch/cancel", {}),
  getIndexingJobs: () => get<IndexingJobSummary[]>("/indexing/jobs"),
  getIndexingHistory: () => get<IndexingRun[]>("/indexing/history"),

  // Chat sessions
  listChats: () => get<ChatSessionSummary[]>("/chats"),
  createChat: () => post<ChatSession>("/chats", {}),
  getChat: (id: string) => get<ChatSession>(`/chats/${id}`),
  updateChat: (id: string, data: { title: string }) =>
    put<ChatSession>(`/chats/${id}`, data),
  deleteChat: (id: string) => del<{ status: string }>(`/chats/${id}`),

  getFileContent: (repoId: string, path: string) =>
    get<{ path: string; content: string }>(`/file?repo_id=${repoId}&path=${encodeURIComponent(path)}`),

  chatMessage: async function* (
    chatId: string,
    question: string,
    repoId?: string,
    topK?: number,
    signal?: AbortSignal,
  ) {
    const res = await fetch(`${BASE}/chats/${chatId}/messages`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ question, repo_id: repoId || undefined, top_k: topK || 40 }),
      signal,
    });

    if (!res.ok) {
      const body = await res.json().catch(() => ({}));
      throw new Error(body.error || `HTTP ${res.status}`);
    }

    const reader = res.body!.getReader();
    const decoder = new TextDecoder();
    let buffer = "";

    while (true) {
      const { done, value } = await reader.read();
      if (done) break;

      buffer += decoder.decode(value, { stream: true });
      const lines = buffer.split("\n");
      buffer = lines.pop() || "";

      let eventType = "";
      for (const line of lines) {
        if (line.startsWith("event: ")) {
          eventType = line.slice(7);
        } else if (line.startsWith("data: ")) {
          const data = line.slice(6);
          yield { event: eventType, data };
          eventType = "";
        }
      }
    }
  },
};
