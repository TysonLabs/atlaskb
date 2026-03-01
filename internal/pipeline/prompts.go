package pipeline

import "fmt"

const systemPromptPhase2 = `You are a code analysis expert. You analyze source files and extract structured knowledge about the codebase.

CRITICAL RULES:
- You MUST respond with valid JSON only — no markdown fences, no commentary outside the JSON.
- You MUST fill in real values — NEVER use "..." or ellipsis as placeholder values.
- Every string value must contain actual content extracted from the code.
- If a file has no entities or facts to extract, use empty arrays [].`

func Phase2Prompt(filePath, language, repoName string, stackInfo StackInfo, content string, roster []EntityEntry) string {
	rosterSection := FormatRosterForPrompt(roster, filePath)
	return fmt.Sprintf(`Analyze this file and extract structured knowledge.

Repository: %s
File: %s
Language: %s
Stack: %v
%s
<file_content>
%s
</file_content>

Respond with JSON in this exact schema:
{
  "file_summary": "one paragraph describing this file's responsibility",
  "entities": [
    {
      "kind": "module|service|function|type|endpoint|concept|config",
      "name": "short name",
      "qualified_name": "repo::package::name",
      "summary": "what this entity does",
      "capabilities": ["list of things this entity can do"],
      "assumptions": ["things this entity assumes to be true"]
    }
  ],
  "facts": [
    {
      "entity_name": "which entity this fact is about (qualified_name)",
      "claim": "a specific, grounded claim",
      "dimension": "what|how|why|when",
      "category": "behavior|constraint|pattern|convention|debt|risk",
      "confidence": "high|medium|low"
    }
  ],
  "relationships": [
    {
      "from": "qualified_name of source entity",
      "to": "qualified_name of target entity",
      "kind": "depends_on|calls|implements|extends|produces|consumes|tested_by|configured_by|owns",
      "description": "brief description",
      "strength": "strong|moderate|weak"
    }
  ]
}

ENTITY RULES:
- Only create entities for EXPORTED/PUBLIC symbols. In Go, exported = starts with
  uppercase letter. DO NOT create entities for unexported (lowercase) functions,
  methods, constants, variables, or struct fields. Examples of what NOT to extract:
  processLoop, dispatch, cleanup, generateID, eventPath, readEvent, calculateDelay,
  worker, matchingHandlers, executeWithRetry, drainChannel, validateTopic.
  These should be described as facts on their parent type, not as separate entities.
- DO NOT create entities for individual method signatures inside an interface definition.
  The interface itself is the entity — list its methods as capabilities on the interface entity
  and add a fact for each method describing its contract.
- Extract ALL exported methods on a CONCRETE type (struct), even simple pass-through/delegation methods.
  A method like ` + "`func (s *FooService) GetByID(ctx, id) { return s.store.GetByID(ctx, id) }`" + `
  IS an entity — it's part of the public API surface.
- DO NOT create entities for variables declared in function bodies (e.g. local vars like
  store, bus, svc, handler, server, logger in main() or init()). Only create entities for
  package-level type declarations, exported functions, and type methods.
- In a main.go or entry-point file, the only entity should be "main" itself. Express
  the wiring and configuration as facts on the main entity, not as separate entities.
- Use exact names from the source code. Never paraphrase or rename functions.
- Express implementation details as facts on the parent entity.

## qualified_name FORMAT (CRITICAL — follow EXACTLY):
Separator rules: "::" between package/module and name, "." between type and method.
NEVER include the repo name, file path, or "src" as a prefix.

**Go:**
  - Package-level type/func: "package::Name"       → e.g. "storage::MemoryStorage", "bus::NewBus"
  - Method on type:          "package::Type.Method" → e.g. "storage::MemoryStorage.Save"
  - DO NOT: "atlaskb-test-repo::storage::MemoryStorage", "src::storage::MemoryStorage"
  - DO NOT: "storage/MemoryStorage", "storage.MemoryStorage" (for types — use :: not . or /)

**Python:**
  - Module-level class/func: "module::Name"          → e.g. "validators::EmailValidator", "auth::authenticate"
  - Method on class:         "module::Class.Method"   → e.g. "validators::EmailValidator.validate"
  - Nested modules use ".":  "utils.helpers::format_date"
  - DO NOT: "src::validators::EmailValidator", "validators.EmailValidator" (for top-level — use ::)

**TypeScript/JavaScript:**
  - Module-level class/func: "module::Name"           → e.g. "channels::HttpChannel", "routes::createRouter"
  - Method on class:         "module::Class.Method"    → e.g. "channels::HttpChannel.send"
  - Use the directory/module name, NOT the file path: "channels" not "src/channels" or "src::channels"
  - DO NOT: "atlaskb-typescript-test-repo::src::channels::HttpChannel"
- If this file references an entity defined in another file, emit facts/relationships
  using its qualified_name — do NOT re-declare the entity.
- BARREL/RE-EXPORT FILES: If a file only re-exports symbols (TypeScript index.ts with
  only "export { ... } from" statements, Python __init__.py with only imports and __all__),
  return EMPTY arrays for entities, facts, and relationships. Only extract from the
  defining file, not re-export files.

FACT RULES:
- Extract 4–10 facts per entity. More complex entities (services, workers, orchestrators,
  main functions) should have closer to 10. Simple pass-through methods can have 4.
- REQUIRED dimensions per entity:
  - "what": at least 1 — what the entity does, its purpose
  - "how": at least 2 — implementation details, patterns, delegation, algorithms
  - "why": at least 1 if rationale is apparent from comments, naming, or design choices
  - "when": extract timing/scheduling facts — polling intervals, timer durations, TTLs,
    cron schedules, retry delays, timeout values, reconciliation frequencies.
    Example: "Reconciles company worker configurations every 10 minutes via a ticker loop"
- OPERATIONAL DETAILS — actively look for and extract:
  - Numeric constants: buffer sizes, pool sizes, concurrency limits, batch sizes, max retries
  - Timeouts and intervals: HTTP timeouts, connection timeouts, polling intervals, backoff durations
  - Thresholds: circuit breaker thresholds, rate limits, queue depth limits, health check intervals
  - Default configuration values: default ports, default queue names, default prefetch counts
  Example: "Uses a ProcessPoolExecutor with max_workers=4 for CPU-bound PII detection"
  Example: "AMQP prefetch_count defaults to 5, matching the concurrency setting"
  Example: "HTTP client timeout is set to 60 seconds with a max of 24 connections"
- LIFECYCLE PATTERNS — extract startup/shutdown/reconciliation sequences:
  - Init → configure → run → cleanup sequences
  - Worker lifecycle: spawn → poll → process → sleep → repeat
  - Recovery flows: detect failure → pause → retry → resume
  Example: "Worker Manager lifecycle: load AppConfig → diff company list → spawn new workers → stop removed workers → sleep 10 min → repeat"
- STATE MACHINES — if an entity transitions through states, document the state transitions
  as facts. Example: "Job states: pending → processing → completed|failed, with failed jobs
  retried up to 3 times before moving to dead-letter queue"
- Flag tech debt (TODOs, FIXMEs, deprecated patterns, missing tests, hardcoded values)
  as category "debt". Flag risks (security, scalability, missing validation) as "risk".
- Every TODO, FIXME, and NOTE comment MUST become a fact with category "debt" or "risk".
- COMMENTS ARE GOLD — extract facts from code comments, docstrings, and inline notes.
  Comments often explain "why" decisions were made and operational constraints.
- CONFIG FILES — if the file is a config file (YAML, TOML, INI, JSON, .env), extract
  every meaningful configuration key as a fact with its default value and purpose.
- Prefer specific claims with concrete values over vague descriptions.
  BAD:  "Uses a retry mechanism"
  GOOD: "Retries failed deliveries up to 5 times with exponential backoff (1s, 2s, 4s, 8s, 16s)"

## FEW-SHOT EXAMPLE

Given this Go file:
` + "```" + `
package worker

import "time"

const (
    reconcileInterval = 10 * time.Minute
    maxRetries        = 3
    httpTimeout       = 30 * time.Second
)

// Manager manages per-company worker goroutines.
// It polls AppConfig for the active company list and reconciles workers accordingly.
type Manager struct {
    config   *AppConfig
    workers  map[string]*Worker
    client   *http.Client
    logger   *log.Logger
}

func NewManager(cfg *AppConfig, logger *log.Logger) *Manager {
    return &Manager{
        config:  cfg,
        workers: make(map[string]*Worker),
        client:  &http.Client{Timeout: httpTimeout},
        logger:  logger,
    }
}

// Run starts the reconciliation loop. It runs until ctx is cancelled.
func (m *Manager) Run(ctx context.Context) error {
    // Initial reconciliation
    if err := m.reconcile(ctx); err != nil {
        m.logger.Printf("initial reconcile failed: %%v", err)
    }
    ticker := time.NewTicker(reconcileInterval)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            m.shutdown()
            return ctx.Err()
        case <-ticker.C:
            if err := m.reconcile(ctx); err != nil {
                m.logger.Printf("reconcile error: %%v", err)
            }
        }
    }
}

// reconcile diffs the AppConfig company list against running workers.
func (m *Manager) reconcile(ctx context.Context) error {
    companies, err := m.config.FetchCompanies(ctx)
    if err != nil {
        return err
    }
    // Spawn new, stop removed
    for _, c := range companies {
        if _, ok := m.workers[c.ID]; !ok {
            m.workers[c.ID] = spawnWorker(c, m.client)
        }
    }
    // TODO: add graceful drain before stopping removed workers
    return nil
}

func (m *Manager) shutdown() {
    for _, w := range m.workers {
        w.Stop()
    }
}
` + "```" + `

Perfect extraction:
{
  "file_summary": "Defines Manager which manages per-company worker goroutines by polling AppConfig on a 10-minute interval and reconciling the active company set.",
  "entities": [
    {"kind": "type", "name": "Manager", "qualified_name": "worker::Manager", "summary": "Manages per-company worker goroutines, polling AppConfig and reconciling on a timer", "capabilities": ["reconcile company workers", "spawn new workers", "stop removed workers", "graceful shutdown"], "assumptions": ["AppConfig provides the authoritative company list", "Each company gets exactly one worker goroutine"]},
    {"kind": "function", "name": "NewManager", "qualified_name": "worker::NewManager", "summary": "Constructor for Manager", "capabilities": ["create Manager with config, empty worker map, and HTTP client"], "assumptions": []},
    {"kind": "function", "name": "Run", "qualified_name": "worker::Manager.Run", "summary": "Starts the reconciliation loop until context is cancelled", "capabilities": ["periodic reconciliation", "graceful shutdown on context cancellation"], "assumptions": ["Context cancellation signals shutdown"]}
  ],
  "facts": [
    {"entity_name": "worker::Manager", "claim": "Manages per-company worker goroutines by polling AppConfig and reconciling the active set", "dimension": "what", "category": "behavior", "confidence": "high"},
    {"entity_name": "worker::Manager", "claim": "Uses a map[string]*Worker keyed by company ID to track running workers", "dimension": "how", "category": "behavior", "confidence": "high"},
    {"entity_name": "worker::Manager", "claim": "HTTP client has a 30-second timeout (httpTimeout constant)", "dimension": "how", "category": "constraint", "confidence": "high"},
    {"entity_name": "worker::Manager", "claim": "maxRetries constant is set to 3", "dimension": "how", "category": "constraint", "confidence": "high"},
    {"entity_name": "worker::Manager", "claim": "Designed so each company has exactly one worker goroutine — no fan-out per company", "dimension": "why", "category": "pattern", "confidence": "medium"},
    {"entity_name": "worker::Manager", "claim": "TODO: add graceful drain before stopping removed workers — currently workers are stopped immediately", "dimension": "how", "category": "debt", "confidence": "high"},
    {"entity_name": "worker::Manager.Run", "claim": "Runs a reconciliation loop using time.NewTicker with a 10-minute interval (reconcileInterval)", "dimension": "when", "category": "behavior", "confidence": "high"},
    {"entity_name": "worker::Manager.Run", "claim": "Lifecycle: initial reconcile → ticker loop → reconcile on tick → shutdown on context cancel", "dimension": "how", "category": "pattern", "confidence": "high"},
    {"entity_name": "worker::Manager.Run", "claim": "Performs an immediate reconciliation on startup before entering the ticker loop", "dimension": "how", "category": "behavior", "confidence": "high"},
    {"entity_name": "worker::Manager.Run", "claim": "Logs reconciliation errors but does not halt the loop — errors are non-fatal", "dimension": "how", "category": "pattern", "confidence": "high"},
    {"entity_name": "worker::Manager.Run", "claim": "Calls m.shutdown() on context cancellation to stop all workers", "dimension": "how", "category": "behavior", "confidence": "high"},
    {"entity_name": "worker::NewManager", "claim": "Initializes an empty worker map and configures the HTTP client with httpTimeout", "dimension": "what", "category": "behavior", "confidence": "high"},
    {"entity_name": "worker::NewManager", "claim": "Uses dependency injection — accepts *AppConfig and *log.Logger", "dimension": "how", "category": "pattern", "confidence": "high"},
    {"entity_name": "worker::NewManager", "claim": "Constructor creates the http.Client inline rather than accepting it as a parameter", "dimension": "how", "category": "behavior", "confidence": "high"},
    {"entity_name": "worker::NewManager", "claim": "Does not perform any validation on the config parameter", "dimension": "how", "category": "risk", "confidence": "medium"}
  ],
  "relationships": [
    {"from": "worker::Manager", "to": "worker::Manager.Run", "kind": "owns", "description": "Run is a method on Manager", "strength": "strong"},
    {"from": "worker::NewManager", "to": "worker::Manager", "kind": "produces", "description": "Constructor that creates Manager", "strength": "strong"},
    {"from": "worker::Manager.Run", "to": "worker::Manager", "kind": "calls", "description": "Run calls m.reconcile and m.shutdown", "strength": "strong"}
  ]
}

Note: sanitizeInput-style unexported functions are NOT entities — they're mentioned as facts on their parent. reconcile and shutdown are unexported so they become facts on Manager and Manager.Run, not separate entities. The example shows 4-5 facts per entity including timing ("when"), lifecycle patterns, numeric constants (httpTimeout=30s, reconcileInterval=10min, maxRetries=3), and a TODO as tech debt.

CRITICAL: Each entity MUST have at least 4 facts AND at least 1 relationship.
- Simple entities (pass-through methods, constructors): 4 facts minimum
- Complex entities (services, workers, main functions, orchestrators): 6-10 facts
- If you cannot think of 4 facts for an entity, you should not create the entity.
- Count your facts and relationships before finalizing.

RELATIONSHIP RULES — EVERY entity MUST have at least 1 relationship:
- METHODS: Always emit "owns" from the struct type to each method entity. This is the easiest
  way to ensure every method has a relationship. Example: Handler → Handler.Publish (owns).
- EMBEDDING: If a struct embeds another struct, emit "extends" from the embedding struct to the embedded struct.
- INTERFACES: If a type implements an interface (has all its methods), emit "implements" from the concrete type to the interface.
- TESTS: If a test function tests a specific entity, emit "tested_by" from the entity to the test function.
  AND emit "calls" from the test function to the entity it tests.
- CALLS: If a method calls another entity's method, emit "calls" from the caller to the callee.
- CONSTRUCTORS: If a function is a constructor (returns a struct), emit "produces" from the function to the struct type.
- TOP-LEVEL FUNCTIONS: Should have "calls" or "depends_on" relationships to the entities they use.
- Count your relationships. If any entity has 0 relationships, add at least an "owns" or "calls" relationship.`, repoName, filePath, language, stackInfo.Languages, rosterSection, content)
}

const systemPromptPhase4 = `You are a software architect. You analyze entities and facts extracted from a codebase and synthesize cross-module insights about architecture, data flows, and contracts.

CRITICAL RULES:
- You MUST respond with valid JSON only — no markdown fences, no commentary outside the JSON.
- Your entire response must start with { and end with }.
- Do NOT output "..." or ellipsis as values. Use real content or empty strings/arrays.
- Do NOT include thinking, reasoning, or explanation text before or after the JSON.`

func Phase4Prompt(repoName string, moduleSummaries string) string {
	return fmt.Sprintf(`Analyze the following extracted entities and facts from the "%s" repository and synthesize cross-module insights.

%s

IMPORTANT: Use only entity qualified_names that exist in the provided context above. Do NOT invent or rename entities. If you need to reference an entity, copy its qualified_name exactly as shown.

Respond with JSON in this exact schema:
{
  "architectural_patterns": [
    {
      "pattern": "name of the pattern",
      "description": "how this pattern is applied",
      "confidence": "high|medium|low"
    }
  ],
  "data_flows": [
    {
      "description": "data flow description",
      "from_module": "source module qualified_name",
      "to_module": "target module qualified_name",
      "mechanism": "how data flows (function call, queue, HTTP, etc.)"
    }
  ],
  "contracts": [
    {
      "between": ["module_a", "module_b"],
      "description": "the contract between these modules",
      "explicit": true
    }
  ],
  "facts": [
    {
      "entity_name": "qualified_name (or repo-level if cross-cutting)",
      "claim": "architectural insight",
      "dimension": "how",
      "category": "pattern|convention|constraint",
      "confidence": "high|medium|low"
    }
  ],
  "relationships": [
    {
      "from": "qualified_name",
      "to": "qualified_name",
      "kind": "depends_on|calls|produces|consumes",
      "description": "brief description",
      "strength": "strong|moderate|weak"
    }
  ]
}`, repoName, moduleSummaries)
}

const systemPromptPhase5 = `You are a technical writer and software architect. You synthesize comprehensive repository summaries from extracted knowledge.

CRITICAL RULES:
- You MUST respond with valid JSON only — no markdown fences, no commentary outside the JSON.
- Your entire response must start with { and end with }.
- Do NOT output "..." or ellipsis as values. Use real content or empty strings/arrays.
- Do NOT include thinking, reasoning, or explanation text before or after the JSON.`

func Phase5Prompt(repoName string, entitySummaries, architecturalFacts, decisions string) string {
	return fmt.Sprintf(`Create a comprehensive summary of the "%s" repository based on the following extracted knowledge.

## Entities
%s

## Architectural Facts
%s

## Decisions
%s

Respond with JSON in this exact schema:
{
  "summary": "2-3 paragraph overview of the repository",
  "capabilities": ["list of what this repository can do"],
  "architecture": "description of the high-level architecture",
  "conventions": [
    {
      "category": "error_handling|testing|naming|logging|other",
      "description": "description of the convention",
      "examples": ["example snippets or references"]
    }
  ],
  "risks_and_debt": ["list of identified risks or tech debt"],
  "key_integration_points": ["list of external dependencies and how they're used"]
}`, repoName, entitySummaries, architecturalFacts, decisions)
}

const systemPromptPhase3 = `You are a code historian extracting architectural decisions from GitHub pull request discussions. PR descriptions and review comments are the richest source of "why" — they capture the rationale, alternatives considered, and tradeoffs that are rarely documented in code.

CRITICAL RULES:
- You MUST respond with valid JSON only — no markdown fences, no commentary outside the JSON.
- Your entire response must start with { and end with }.
- Do NOT output "..." or ellipsis as values. Use real content or empty strings/arrays.
- Do NOT include thinking, reasoning, or explanation text before or after the JSON.`

func Phase3Prompt(repoName string, prsText string, entityRoster string) string {
	return fmt.Sprintf(`Analyze the following merged pull requests from the "%s" repository. Extract architectural decisions, rationale, and "why" dimension facts from PR descriptions and review discussions.

## Known Entities
%s

## Pull Requests
%s

Respond with JSON in this exact schema:
{
  "facts": [
    {
      "entity_name": "qualified_name of the affected entity (or repo name if repo-level)",
      "claim": "what was decided/changed and why",
      "dimension": "why|when|what|how",
      "category": "behavior|constraint|pattern|convention|debt|risk",
      "confidence": "high|medium|low"
    }
  ],
  "decisions": [
    {
      "summary": "one-line decision description",
      "description": "fuller context from the PR description",
      "rationale": "why this decision was made (from PR body, reviews, or issue context)",
      "alternatives": [
        {
          "description": "an alternative that was considered",
          "rejected_because": "why it was rejected"
        }
      ],
      "tradeoffs": ["tradeoff 1", "tradeoff 2"],
      "pr_number": 42,
      "made_at": "ISO timestamp of PR merge"
    }
  ]
}

FACT RULES:
- Prefer "why" and "when" dimensions — these are the hardest to extract from code alone.
- For entity_name, use exact qualified_names from the entity roster above when possible.
- If the fact is repo-level, use the repository name "%s".
- Extract rationale from PR descriptions, review comments, and linked issue context.
- Operational decisions (performance, scaling, security) are high value.

DECISION RULES:
- PRs that add/change architecture, switch libraries, modify data models, or change APIs represent DECISIONS.
- Extract alternatives from review discussions where reviewers suggested different approaches.
- Extract tradeoffs when PR authors explain what they gained vs. what they gave up.
- Include the pr_number field for provenance tracking.
- Every PR batch should yield at least 1-2 decisions unless all PRs are trivial.`, repoName, entityRoster, prsText, repoName)
}

const systemPromptGitLog = `You are a code historian. You analyze git commit history to extract the "when" and "why" dimensions of a codebase's evolution.

CRITICAL RULES:
- You MUST respond with valid JSON only — no markdown fences, no commentary outside the JSON.
- Your entire response must start with { and end with }.
- Do NOT output "..." or ellipsis as values. Use real content or empty strings/arrays.
- Do NOT include thinking, reasoning, or explanation text before or after the JSON.`

func GitLogPrompt(repoName string, commits string) string {
	return fmt.Sprintf(`Analyze the following git history from the "%s" repository. Extract facts about the evolution, significant changes, and decision rationale visible in the commit history.

%s

Respond with JSON in this exact schema:
{
  "facts": [
    {
      "entity_name": "qualified_name of the affected entity (or repo name if repo-level)",
      "claim": "what happened and when",
      "dimension": "when",
      "category": "behavior|pattern|convention",
      "confidence": "high|medium|low"
    }
  ],
  "decisions": [
    {
      "summary": "one-line decision description",
      "description": "fuller context",
      "rationale": "why this was done",
      "made_at": "ISO timestamp if determinable"
    }
  ]
}

FACT RULES:
- For entity_name, use the repository name "%s" if the fact is repo-level.
- Do NOT invent entity names not present in the codebase. If unsure, use the repo name.

DECISION RULES:
- Commits that add/change dependencies, switch libraries, modify architecture,
  or change configuration represent DECISIONS.
- Infer rationale from change type and affected files even if message is terse.
- Every repository should have decisions about tech stack, patterns, and structure.`, repoName, commits, repoName)
}
