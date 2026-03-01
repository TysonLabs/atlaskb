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
- Extract at least TWO facts per entity: one "what" (behavior/capability) and one
  "how" (implementation detail, pattern used, or delegation strategy).
- Extract a "why" fact per entity if rationale is apparent from comments or naming.
- Flag tech debt (TODOs, FIXMEs, deprecated patterns, missing tests, hardcoded values)
  as category "debt". Flag risks (security, scalability, missing validation) as "risk".
- Every TODO, FIXME, and NOTE comment MUST become a fact with category "debt" or "risk".
- Prefer specific claims over vague ones.

## FEW-SHOT EXAMPLE

Given this Go file:
` + "```" + `
package tasks

type TaskStore interface {
    GetTask(ctx context.Context, id string) (*Task, error)
    ListTasks(ctx context.Context) ([]*Task, error)
}

type TaskHandler struct {
    store TaskStore
    logger *log.Logger
}

func NewTaskHandler(store TaskStore, logger *log.Logger) *TaskHandler {
    return &TaskHandler{store: store, logger: logger}
}

func (h *TaskHandler) GetTask(ctx context.Context, id string) (*Task, error) {
    return h.store.GetTask(ctx, id)
}

func (h *TaskHandler) ListTasks(ctx context.Context) ([]*Task, error) {
    return h.store.ListTasks(ctx)
}

func (h *TaskHandler) DeleteTask(ctx context.Context, id string) error {
    h.logger.Printf("deleting task %%s", id)
    return h.store.Delete(ctx, id)
}

func sanitizeInput(s string) string {
    return strings.TrimSpace(s)
}
` + "```" + `

Perfect extraction:
{
  "file_summary": "Defines TaskHandler which implements task CRUD operations by delegating to a TaskStore interface.",
  "entities": [
    {"kind": "type", "name": "TaskStore", "qualified_name": "tasks::TaskStore", "summary": "Interface defining task storage operations", "capabilities": ["get task by ID", "list all tasks"], "assumptions": []},
    {"kind": "type", "name": "TaskHandler", "qualified_name": "tasks::TaskHandler", "summary": "Handles task operations by delegating to a TaskStore", "capabilities": ["get task", "list tasks", "delete task with logging"], "assumptions": ["TaskStore implementation is injected at construction"]},
    {"kind": "function", "name": "NewTaskHandler", "qualified_name": "tasks::NewTaskHandler", "summary": "Constructor for TaskHandler", "capabilities": ["create TaskHandler with store and logger"], "assumptions": []},
    {"kind": "function", "name": "GetTask", "qualified_name": "tasks::TaskHandler.GetTask", "summary": "Returns a task by ID, delegating to the store", "capabilities": ["retrieve single task"], "assumptions": []},
    {"kind": "function", "name": "ListTasks", "qualified_name": "tasks::TaskHandler.ListTasks", "summary": "Returns all tasks, delegating to the store", "capabilities": ["retrieve all tasks"], "assumptions": []},
    {"kind": "function", "name": "DeleteTask", "qualified_name": "tasks::TaskHandler.DeleteTask", "summary": "Deletes a task by ID with logging", "capabilities": ["delete task", "log deletion"], "assumptions": []}
  ],
  "facts": [
    {"entity_name": "tasks::TaskStore", "claim": "Defines two operations: GetTask (by ID) and ListTasks (all)", "dimension": "what", "category": "behavior", "confidence": "high"},
    {"entity_name": "tasks::TaskStore", "claim": "Interface with no implementation in this file", "dimension": "how", "category": "pattern", "confidence": "high"},
    {"entity_name": "tasks::TaskHandler", "claim": "Handles task CRUD by delegating to a TaskStore", "dimension": "what", "category": "behavior", "confidence": "high"},
    {"entity_name": "tasks::TaskHandler", "claim": "Uses unexported helper sanitizeInput for input cleaning", "dimension": "how", "category": "behavior", "confidence": "medium"},
    {"entity_name": "tasks::TaskHandler.GetTask", "claim": "Pure pass-through to store.GetTask with no additional logic", "dimension": "how", "category": "behavior", "confidence": "high"},
    {"entity_name": "tasks::TaskHandler.GetTask", "claim": "Retrieves a single task by its ID", "dimension": "what", "category": "behavior", "confidence": "high"},
    {"entity_name": "tasks::TaskHandler.ListTasks", "claim": "Pure pass-through to store.ListTasks with no additional logic", "dimension": "how", "category": "behavior", "confidence": "high"},
    {"entity_name": "tasks::TaskHandler.DeleteTask", "claim": "Logs deletion before delegating to store", "dimension": "how", "category": "behavior", "confidence": "high"},
    {"entity_name": "tasks::TaskHandler.DeleteTask", "claim": "Only method that adds behavior (logging) beyond pure delegation", "dimension": "why", "category": "pattern", "confidence": "medium"}
  ],
  "relationships": [
    {"from": "tasks::TaskHandler", "to": "tasks::TaskStore", "kind": "depends_on", "description": "TaskHandler delegates all storage operations to TaskStore", "strength": "strong"},
    {"from": "tasks::NewTaskHandler", "to": "tasks::TaskHandler", "kind": "produces", "description": "Constructor that creates TaskHandler", "strength": "strong"},
    {"from": "tasks::TaskHandler", "to": "tasks::TaskHandler.GetTask", "kind": "owns", "description": "GetTask is a method on TaskHandler", "strength": "strong"},
    {"from": "tasks::TaskHandler", "to": "tasks::TaskHandler.ListTasks", "kind": "owns", "description": "ListTasks is a method on TaskHandler", "strength": "strong"},
    {"from": "tasks::TaskHandler", "to": "tasks::TaskHandler.DeleteTask", "kind": "owns", "description": "DeleteTask is a method on TaskHandler", "strength": "strong"},
    {"from": "tasks::TaskHandler.GetTask", "to": "tasks::TaskStore", "kind": "calls", "description": "Delegates to store.GetTask", "strength": "strong"},
    {"from": "tasks::TaskHandler.DeleteTask", "to": "tasks::TaskStore", "kind": "calls", "description": "Delegates to store.Delete", "strength": "strong"}
  ]
}

Note: sanitizeInput is unexported (lowercase) so it is NOT an entity — it's mentioned as a fact on TaskHandler instead. All exported methods including simple pass-throughs (GetTask, ListTasks) are extracted as entities. TaskStore is an interface so its methods are described as facts, not separate entities.

CRITICAL: Each entity MUST have at least 2 facts AND at least 1 relationship. If you cannot think of 2 facts for an entity, you should not create the entity. Count your facts and relationships before finalizing.

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
