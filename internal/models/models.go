package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
)

// Ptr returns a pointer to the given value. Useful for setting nullable struct fields.
func Ptr[T any](v T) *T { return &v }

// Entity kinds
const (
	EntityModule   = "module"
	EntityService  = "service"
	EntityFunction = "function"
	EntityType     = "type"
	EntityEndpoint = "endpoint"
	EntityConcept  = "concept"
	EntityConfig   = "config"
)

// Fact dimensions
const (
	DimensionWhat = "what"
	DimensionHow  = "how"
	DimensionWhy  = "why"
	DimensionWhen = "when"
)

// Fact categories
const (
	CategoryBehavior   = "behavior"
	CategoryConstraint = "constraint"
	CategoryPattern    = "pattern"
	CategoryConvention = "convention"
	CategoryDebt       = "debt"
	CategoryRisk       = "risk"
)

// Confidence levels
const (
	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceLow    = "low"
)

// Relationship kinds
const (
	RelDependsOn    = "depends_on"
	RelCalls        = "calls"
	RelImplements   = "implements"
	RelExtends      = "extends"
	RelProduces     = "produces"
	RelConsumes     = "consumes"
	RelReplacedBy   = "replaced_by"
	RelTestedBy     = "tested_by"
	RelConfiguredBy = "configured_by"
	RelOwns         = "owns"
)

// Relationship strengths
const (
	StrengthStrong   = "strong"
	StrengthModerate = "moderate"
	StrengthWeak     = "weak"
)

// Job statuses
const (
	JobPending    = "pending"
	JobInProgress = "in_progress"
	JobCompleted  = "completed"
	JobFailed     = "failed"
	JobSkipped    = "skipped"
)

// Job phases
const (
	PhasePhase1    = "phase1"
	PhasePhase2    = "phase2"
	PhasePhase4    = "phase4"
	PhasePhase5    = "phase5"
	PhaseGitLog    = "gitlog"
	PhaseEmbedding = "embedding"
)

type Repo struct {
	ID            uuid.UUID  `json:"id"`
	Name          string     `json:"name"`
	RemoteURL     *string    `json:"remote_url,omitempty"`
	LocalPath     string     `json:"local_path"`
	DefaultBranch string     `json:"default_branch"`
	LastCommitSHA *string    `json:"last_commit_sha,omitempty"`
	LastIndexedAt *time.Time `json:"last_indexed_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type Entity struct {
	ID            uuid.UUID `json:"id"`
	RepoID        uuid.UUID `json:"repo_id"`
	Kind          string    `json:"kind"`
	Name          string    `json:"name"`
	QualifiedName string    `json:"qualified_name"`
	Path          *string   `json:"path,omitempty"`
	Summary       *string   `json:"summary,omitempty"`
	Capabilities  []string  `json:"capabilities,omitempty"`
	Assumptions   []string  `json:"assumptions,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type Provenance struct {
	SourceType string `json:"source_type"`
	Repo       string `json:"repo"`
	Ref        string `json:"ref"`
	URL        string `json:"url,omitempty"`
	Excerpt    string `json:"excerpt,omitempty"`
	AnalyzedAt string `json:"analyzed_at"`
}

type Fact struct {
	ID           uuid.UUID       `json:"id"`
	EntityID     uuid.UUID       `json:"entity_id"`
	RepoID       uuid.UUID       `json:"repo_id"`
	Claim        string          `json:"claim"`
	Dimension    string          `json:"dimension"`
	Category     string          `json:"category"`
	Confidence   string          `json:"confidence"`
	Provenance   []Provenance    `json:"provenance"`
	Embedding    pgvector.Vector `json:"-"`
	SupersededBy *uuid.UUID      `json:"superseded_by,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

type Alternative struct {
	Description     string `json:"description"`
	RejectedBecause string `json:"rejected_because"`
}

type Decision struct {
	ID           uuid.UUID     `json:"id"`
	RepoID       uuid.UUID     `json:"repo_id"`
	Summary      string        `json:"summary"`
	Description  string        `json:"description"`
	Rationale    string        `json:"rationale"`
	Alternatives []Alternative `json:"alternatives"`
	Tradeoffs    []string      `json:"tradeoffs"`
	Provenance   []Provenance  `json:"provenance"`
	MadeAt       *time.Time    `json:"made_at,omitempty"`
	StillValid   bool          `json:"still_valid"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

type Relationship struct {
	ID           uuid.UUID    `json:"id"`
	RepoID       uuid.UUID    `json:"repo_id"`
	FromEntityID uuid.UUID    `json:"from_entity_id"`
	ToEntityID   uuid.UUID    `json:"to_entity_id"`
	Kind         string       `json:"kind"`
	Description  *string      `json:"description,omitempty"`
	Strength     string       `json:"strength"`
	Provenance   []Provenance `json:"provenance"`
	CreatedAt    time.Time    `json:"created_at"`
}

type ExtractionJob struct {
	ID           uuid.UUID  `json:"id"`
	RepoID       uuid.UUID  `json:"repo_id"`
	Phase        string     `json:"phase"`
	Target       string     `json:"target"`
	ContentHash  *string    `json:"content_hash,omitempty"`
	Status       string     `json:"status"`
	ErrorMessage *string    `json:"error_message,omitempty"`
	TokensUsed   *int       `json:"tokens_used,omitempty"`
	CostUSD      *float64   `json:"cost_usd,omitempty"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}
