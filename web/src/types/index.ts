export interface Repo {
  id: string;
  name: string;
  remote_url?: string;
  local_path: string;
  default_branch: string;
  exclude_dirs: string[];
  last_commit_sha?: string;
  last_indexed_at?: string;
  created_at: string;
  updated_at: string;
}

export interface RepoListItem extends Repo {
  entity_count: number;
  fact_count: number;
  relationship_count: number;
  decision_count: number;
  quality_overall?: number;
}

export interface RepoDetail extends Repo {
  entity_count: number;
  entity_by_kind: Record<string, number>;
  fact_count: number;
  fact_by_dimension: Record<string, number>;
  relationship_count: number;
  decision_count: number;
  quality_overall?: number;
  quality_entity_cov?: number;
  quality_fact_density?: number;
  quality_rel_connect?: number;
  quality_dim_coverage?: number;
  quality_parse_rate?: number;
}

export interface Entity {
  id: string;
  repo_id: string;
  kind: string;
  name: string;
  qualified_name: string;
  path?: string;
  summary?: string;
  capabilities?: string[];
  assumptions?: string[];
  created_at: string;
  updated_at: string;
}

export interface Provenance {
  source_type: string;
  repo: string;
  ref: string;
  url?: string;
  excerpt?: string;
  analyzed_at: string;
}

export interface Fact {
  id: string;
  entity_id: string;
  repo_id: string;
  claim: string;
  dimension: string;
  category: string;
  confidence: string;
  provenance: Provenance[];
  superseded_by?: string;
  created_at: string;
  updated_at: string;
}

export interface Relationship {
  id: string;
  repo_id: string;
  from_entity_id: string;
  to_entity_id: string;
  kind: string;
  description?: string;
  strength: string;
  provenance: Provenance[];
  created_at: string;
  from_entity_name?: string;
  to_entity_name?: string;
}

export interface Alternative {
  description: string;
  rejected_because: string;
}

export interface Decision {
  id: string;
  repo_id: string;
  summary: string;
  description: string;
  rationale: string;
  alternatives: Alternative[];
  tradeoffs: string[];
  provenance: Provenance[];
  made_at?: string;
  still_valid: boolean;
  created_at: string;
  updated_at: string;
}

export interface IndexingRun {
  id: string;
  repo_id: string;
  commit_sha?: string;
  mode: string;
  model_extraction?: string;
  model_synthesis?: string;
  files_total?: number;
  files_analyzed?: number;
  files_skipped?: number;
  entities_created?: number;
  facts_created?: number;
  rels_created?: number;
  decisions_created?: number;
  quality_overall?: number;
  quality_entity_cov?: number;
  quality_fact_density?: number;
  quality_rel_connect?: number;
  quality_dim_coverage?: number;
  quality_parse_rate?: number;
  duration_ms?: number;
  started_at: string;
  completed_at?: string;
  created_at: string;
  repo_name?: string;
}

export interface Stats {
  repos: number;
  entities: number;
  facts: number;
  relationships: number;
  decisions: number;
}

export interface SearchResult {
  fact: Fact;
  entity: Entity;
  repo_name: string;
  score: number;
  source: string;
}

export interface EntityDetail extends Entity {
  facts: Fact[];
  relationships: Relationship[];
}

export interface GraphNode {
  id: string;
  name: string;
  kind: string;
  path?: string;
  repoId?: string;
  repoName?: string;
}

export interface GraphEdge {
  id: string;
  source: string;
  target: string;
  kind: string;
  strength: string;
  description?: string;
  crossRepo?: boolean;
}

export interface CrossRepoLink {
  id: string;
  from_entity_id: string;
  to_entity_id: string;
  from_repo_id: string;
  to_repo_id: string;
  kind: string;
  strength: string;
  description?: string;
  provenance: Provenance[];
  created_at: string;
}

export interface GraphData {
  nodes: GraphNode[];
  edges: GraphEdge[];
}

export interface EntitySearchResult {
  items: Entity[];
  total: number;
}

export interface ChatMessage {
  id: string;
  role: "user" | "assistant";
  content: string;
  evidence?: SearchResult[];
  timestamp: string;
}

export interface ChatSession {
  id: string;
  title: string;
  messages: ChatMessage[];
  last_usage?: {
    prompt_tokens: number;
    completion_tokens: number;
    total_tokens: number;
    context_window: number;
  };
  created_at: string;
  updated_at: string;
}

export interface ChatSessionSummary {
  id: string;
  title: string;
  message_count: number;
  created_at: string;
  updated_at: string;
}

export interface BatchRepoStatus {
  repo_id: string;
  repo_name: string;
  status: "pending" | "running" | "completed" | "failed";
  logs: string[];
}

export interface BatchStatus {
  active: boolean;
  id: string;
  total: number;
  completed: number;
  failed: number;
  current_index: number;
  force: boolean;
  repos: BatchRepoStatus[];
}

export interface IndexingJobSummary {
  repo_id: string;
  repo_name: string;
  status: string;
  latest_log: string;
  is_batch: boolean;
}
