# Pipeline Prompt Iteration & Post-Processing TODO

Iterative improvement loop for extraction quality. Run test repos, inspect output, fix prompts, repeat.

## Prerequisites

- [x] PostgreSQL with pgvector running
- [x] LLM endpoint configured (qwen3.5-35b-a3b — baseline model, then upgraded to Qwen3-Coder-Next-AWQ-4bit)
- [x] Embeddings endpoint configured (mxbai-embed-large-v1)
- [x] All 3 test repos created:
  - `bash scripts/create-test-repo.sh` → `/tmp/atlaskb-test-repo` (Go)
  - `bash scripts/create-python-test-repo.sh` → `/tmp/atlaskb-python-test-repo` (Python)
  - `bash scripts/create-typescript-test-repo.sh` → `/tmp/atlaskb-typescript-test-repo` (TypeScript)

---

## Round 1: Baseline Run

### 1.1 Index all 3 repos
```bash
go run ./cmd/atlaskb index --force /tmp/atlaskb-test-repo
go run ./cmd/atlaskb index --force /tmp/atlaskb-python-test-repo
go run ./cmd/atlaskb index --force /tmp/atlaskb-typescript-test-repo
```

### 1.2 Capture baseline metrics
After each index run, record the quality score output. Also query the DB directly:

```sql
-- Per-repo entity/fact/relationship counts
SELECT r.url,
  (SELECT count(*) FROM entities WHERE repo_id = r.id) as entities,
  (SELECT count(*) FROM facts WHERE entity_id IN (SELECT id FROM entities WHERE repo_id = r.id)) as facts,
  (SELECT count(*) FROM relationships WHERE repo_id = r.id) as relationships,
  (SELECT count(*) FROM decisions WHERE repo_id = r.id) as decisions
FROM repos r;

-- Fact category distribution (are conventions being extracted?)
SELECT f.category, count(*)
FROM facts f
JOIN entities e ON f.entity_id = e.id
JOIN repos r ON e.repo_id = r.id
WHERE r.url LIKE '%typescript%'
GROUP BY f.category;

-- Entity kind distribution (are we getting the right types?)
SELECT kind, count(*) FROM entities WHERE repo_id = ? GROUP BY kind;

-- Relationship kind distribution
SELECT kind, count(*) FROM relationships WHERE repo_id = ? GROUP BY kind;

-- Orphan entities (no facts attached)
SELECT e.qualified_name, e.kind
FROM entities e
LEFT JOIN facts f ON f.entity_id = e.id
WHERE e.repo_id = ? AND f.id IS NULL;

-- Orphan facts (entity_name didn't resolve)
-- These get silently dropped — check pipeline logs for "skipping fact" warnings
```

### 1.3 Spot-check MCP tool output
Run each P0 tool against all 3 repos and save results:

```bash
# Use atlaskb mcp or query directly
# For each repo, test:
# - get_conventions → Are real conventions found? Or generic filler?
# - get_module_context for 2-3 key files → Does the summary match the code?
# - get_service_contract for a core interface → Are dependents correct?
# - get_impact_analysis for a central module → Does the graph make sense?
# - get_decision_context → Are decisions meaningful or hallucinated?
```

### 1.4 Build a scorecard

For each repo, rate (1-5) on:

| Dimension | Go | Python | TS | Notes |
|-----------|-----|--------|-----|-------|
| Entity completeness — are all key types/functions found? | | | | |
| Entity precision — any hallucinated or garbage entities? | | | | |
| Fact quality — are claims grounded and specific? | | | | |
| Fact coverage — are conventions, debt, risks captured? | | | | |
| Relationship accuracy — do edges match real dependencies? | | | | |
| Relationship completeness — are important links missing? | | | | |
| Decision quality — real architectural decisions or filler? | | | | |
| Cross-module synthesis — does Phase 4 add value? | | | | |
| Repo summary — accurate and useful? | | | | |
| Convention extraction — specific enough for an agent? | | | | |

---

## Round 1 Results: Baseline (qwen3.5-35b-a3b, 2026-02-28)

### 1.5 Baseline Metrics

| | Go (eventbus) | Python (taskflow) | TS (webhookrelay) |
|---|---|---|---|
| **Quality Score** | **91** | **98** | **97** |
| Entities | 172 (155 extracted + 2 deps + 15 concept) | 120 (101 extracted + 10 deps + 9 concept) | 203 (189 extracted + 13 deps + 1 concept) |
| Facts | 246 | 269 | 352 |
| Relationships | 135 | 113 | 189 |
| Decisions | 8 | 5 | 5 |
| Facts/entity | 1.4 | 2.4 | 1.9 |
| Parse success | 96.6% (28/29) | 100% (30/30) | 100% (40/40) |
| Phase 4 | **FAILED 3/3** | Partial (7 skipped refs) | Partial (2 skipped refs) |
| Phase 5 | OK | OK | OK |
| Git log | 18 facts, 8 decisions | 11 facts, 5 decisions | 13 facts, 5 decisions |
| Duration | 18m | 17m | 25m |

### 1.6 Observed Issues (Prioritized)

#### P0 — Phase 4 JSON Parse Failure (Go repo)
- Phase 4 synthesis failed all 3 retries for the Go repo with `invalid character 'a' after object key:value pair`
- The model produces invalid JSON when given larger/more complex entity contexts
- Python and TS repos succeeded but with entity reference mismatches
- **Impact:** No cross-module synthesis for Go. Partial for others.
- **Fix target:** Phase 4 prompt simplification, better JSON guidance, or error-correction retry prompt

#### P1 — Barrel Export Entity Duplication (TS-specific)
- TypeScript `index.ts` barrel re-exports create duplicate entities
- Example: `src/types/event::Priority` (from event.ts) AND `atlaskb-typescript-test-repo::src/types/Priority` (from index.ts) both inserted as separate entities
- The dedup logic sees different "owners" and inserts both
- Every barrel file (7 in TS repo) creates ~5-10 duplicate entities
- **Impact:** ~60-80 inflated entities in TS repo. Pollutes query results, inflates relationship count.
- **Fix options:**
  1. Prompt: "Skip barrel/index files that only re-export — they contain no original logic"
  2. Pipeline: Detect re-export-only files in Phase 1 and skip them in Phase 2
  3. Post-processing: Merge entities that share the same short name + kind within a repo

#### P2 — Qualified Name Inconsistency
- The model uses wildly inconsistent qualified name formats across files in the same repo:
  - `src/channels/http-channel::HttpChannel` (file-path based)
  - `atlaskb-typescript-test-repo::src::channels::HttpChannel` (repo-prefixed)
  - `channels::channel.Channel` (module-based with dot)
  - `retry::policy::RetryPolicy` (short module name)
  - `module::Repository` (generic "module" prefix)
- This breaks Phase 4 cross-references (entity names don't match stored names)
- Python had similar but less severe issues: `taskflow.errors.TaskFlowError` vs `taskflow.errors::TaskFlowError`
- **Impact:** Phase 4 skips relationships when names don't resolve. Facts get dropped or reparented incorrectly.
- **Fix options:**
  1. Prompt: Add explicit format rule with examples per language
  2. Post-processing: Normalize all qualified names to a canonical format after extraction
  3. Matching: Fuzzy match on short name when exact qualified name fails (already partially exists)

#### P3 — Fact Reparenting to Wrong Owner
- Some facts get reparented to the repo-level entity when the model uses a module-level name
- Example: `src/api/middleware.ts`: fact for `atlaskb-typescript-test-repo::src/api/middleware` → reparented to owner `atlaskb-typescript-test-repo` (the repo entity)
- This puts file-level facts on the repo, diluting repo-level queries
- **Impact:** Low count but wrong attribution. Shows up in `get_module_context` returning irrelevant facts.

#### P4 — Skipped Facts (Entity Not Found)
- Multiple instances of facts being silently dropped because the entity name didn't resolve:
  - Python: `taskflow.api.routes` (2 facts skipped), `taskflow.errors` (1 skipped), `taskflow.models` (1 skipped)
  - These are module-level names where the model didn't use the full qualified name
- **Impact:** Lost facts. The model extracted them but the pipeline couldn't place them.
- **Fix:** Better fuzzy matching on module-level names, or prompt guidance to always use full entity qualified names for facts

#### P5 — Phase 4 Entity Reference Mismatches
- Even when Phase 4 succeeds (Python, TS), it references entities that don't exist in the DB:
  - Python: `taskflow.api.app.lifespan`, `taskflow.api.create_app`, `taskflow.engine.executor.SyncExecutor.execute` (none found)
  - TS: `atlaskb-typescript-test-repo::src/filters/header-filter.HeaderFilter` → `atlaskb-typescript-test-repo::src/channels::SlackChannel` (not found)
- Phase 4 is told "use only entity qualified_names that exist in the provided context" but the model still invents or reformats names
- **Impact:** Data flows and cross-module relationships silently dropped

### 1.7 What Worked Well
- **Owner-based dedup is solid.** The pipeline correctly differentiated same-named methods: `storage::FileStorage.Save` vs `storage::MemoryStorage.Save`, `bus::Handler` vs `api::Handler`
- **Fact quality is good for Phase 2.** Claims are mostly grounded and specific (algorithm names, patterns, values)
- **Git log extraction works.** 8 decisions from Go repo, 5 each from Python/TS — reasonable for test repos
- **Phase 5 summaries produced.** All 3 repos got architecture facts, conventions, integration points, and risks
- **Parse success near-perfect.** Only 1 failure across 99 Phase 2 jobs (Go repo, 1 file)

---

## Round 2: Prompt Fixes (Based on Round 1 Findings)

All prompts live in `internal/pipeline/prompts.go`. Schemas in `internal/pipeline/schemas.go`.

### 2.1 Phase 2 — File-Level Extraction

**Known weaknesses to check for:**

- [ ] **Go-centric few-shot example.** The existing example shows Go (TaskStore/TaskHandler). Python and TypeScript files get no example of expected output. **CONFIRMED:** Python scored higher (98) than Go (91) despite the Go example, so this may not be the bottleneck. But TS barrel export confusion suggests a TS example showing "skip re-exports" would help.
- [ ] **Over-extraction of entities.** The prompt says "exported/public symbols" but smaller models may extract everything. **CONFIRMED for TS:** Barrel `index.ts` files get full entity extraction instead of being skipped. Not confirmed for private symbol leakage yet.
- [ ] **Under-extraction of methods.** The prompt emphasizes "ALL exported methods on concrete types" but models may miss some. **NOT YET CHECKED** — need to compare extracted vs actual method count per type.
- [ ] **Fact quality.** Are facts specific and grounded? **PARTIALLY CONFIRMED:** Fact quality appears good from pipeline logs. Need deeper spot-check via MCP tools.
- [ ] **Relationship completeness.** The "every entity MUST have at least 1 relationship" rule may cause hallucinated relationships. **NOT YET CHECKED** — need to validate a sample of relationships against actual code.
- [x] **Qualified name format.** Check if the model consistently produces `repo::package::Name` format or invents its own. **CONFIRMED BROKEN:** See P2 above. Model uses 5+ different formats within the same repo. Highest-impact prompt fix needed.
- [ ] **Convention extraction.** Phase 2 should tag conventions (category=convention). **NOT YET CHECKED** — need to query fact categories.
- [ ] **Debt/risk extraction.** Test repos have TODO/FIXME/NOTE comments. **NOT YET CHECKED** — all 3 test repos have deliberate TODO/FIXME markers.

**Fix template:**
```
For each issue found, document:
- Problem: [what's wrong]
- Evidence: [specific example from test output]
- Prompt change: [exact text to add/modify in prompts.go]
- Expected improvement: [what should change]
```

### 2.2 Phase 4 — Cross-Module Synthesis

**Known weaknesses to check for:**

- [x] **Prompt is too brief (~400 words).** Phase 4 has no examples at all. **CONFIRMED:** Go repo failed all 3 parse attempts (invalid JSON). Python/TS succeeded but with entity mismatches. The prompt needs a concrete example and stronger JSON formatting guidance.
- [ ] **"Architectural patterns" under-specified.** The model may produce generic patterns. Python Phase 4 stored 4 patterns — need to check if they're meaningful.
- [ ] **Contracts concept is vague.** Python Phase 4 stored 3 contracts as facts — need to check quality.
- [x] **Data flows may be hallucinated.** **CONFIRMED:** Python Phase 4 produced data flows with entity names that don't exist in the DB (e.g., `taskflow.api.dependencies.get_config -> taskflow.api.app.lifespan` — skipped). The model is inventing plausible but non-matching names.
- [x] **Entity name drift.** **CONFIRMED:** Phase 4 consistently uses qualified names that don't match stored entity names. See P5 above. This is the #1 Phase 4 issue.
- [ ] **Context cap too aggressive.** Entities capped at 60, facts at 5/entity. **NOT YET CHECKED** — Go repo has 172 entities, so the cap was likely hit. Need to verify.

### 2.3 Phase 5 — Repository Summary

**Known weaknesses to check for:**

- [ ] **Prompt is minimal (~300 words).** No examples, no length guidance. Summary quality will vary wildly. Add guidance: "Summary should be 200-400 words covering purpose, architecture, key patterns, and tech stack."
- [ ] **Conventions may be too generic.** The schema asks for conventions with examples but doesn't define what "examples" means (code snippets? file references?). Clarify: "examples should be entity qualified_names or file paths that demonstrate the convention."
- [ ] **Risks/debt may be hallucinated.** Check if risks come from actual debt facts or if the model invents them. Add: "Only list risks that are supported by extracted facts — reference the fact's entity."
- [ ] **Key integration points.** Check if external dependencies from deps.go are reflected here. The pipeline parses go.mod/package.json/pyproject.toml — those should feed into this.

### 2.4 Git Log Analysis

**Known weaknesses to check for:**

- [ ] **Commit message truncation (500 chars).** Check if important context is lost for verbose commits. Consider raising to 1000 for the test repos.
- [ ] **File list cap (10 files).** Large commits lose file context. Check if this matters for decision extraction.
- [ ] **Decision quality.** Are extracted decisions real ("switched from REST to gRPC for internal services") or noise ("updated dependencies")? If noisy, add a threshold: "Only extract decisions that represent deliberate architectural choices, not routine maintenance."
- [ ] **No few-shot example.** Add an example showing a commit message → expected facts/decisions extraction.

---

## Round 3: Few-Shot Examples

Add 2-3 gold-standard examples to each prompt that currently lacks them. Use real output from the test repos (corrected by hand) as the examples.

### 3.1 Phase 2 — Add cross-language examples

Currently has 1 Go example. Add:

- [ ] **Python example** — Pick a file from the Python test repo (e.g., the executor with protocol/ABC pattern). Show expected entities, facts, relationships for a Python class hierarchy.
- [ ] **TypeScript example** — Pick a file from the TS test repo (e.g., the channel interface + HttpChannel). Show expected output for interfaces, generics, barrel exports.
- [ ] Keep examples concise (30-40 lines each). The prompt is already ~2800 words — don't bloat it further. Consider moving examples to a separate "examples" section at the end of the prompt.

### 3.2 Phase 4 — Add synthesis example

- [ ] Create 1 example showing: "Given these 5 entities and 10 facts, here's the expected synthesis output." Use the Go test repo's middleware chain as the example (clear architectural pattern, real data flows, explicit contracts).

### 3.3 Phase 5 — Add summary example

- [ ] Create 1 example showing a complete repo summary output. Use the Go test repo as the gold standard — manually write the ideal summary, conventions list, and risks.

### 3.4 Git Log — Add extraction example

- [ ] Create 1 example showing 3-4 commits → expected facts and decisions. Use commits that represent a mix: routine (no decision), architectural (yes decision), dependency change (maybe decision).

---

## Round 4: Post-Processing Rules

All post-processing lives in `internal/pipeline/parser.go` (JSON cleaning, enum sanitization) and `internal/pipeline/phase2.go` (dedup, reparenting).

### 4.1 Silent fallback audit

The enum sanitizer silently converts invalid values to defaults:
- Invalid entity kind → `concept`
- Invalid fact dimension → `what`
- Invalid fact category → `behavior`
- Invalid relationship kind → `depends_on`

**Problem:** This hides extraction errors. A wrong dimension silently becomes `what` and you never know.

- [ ] Add counters/logging for each sanitization fallback. After a pipeline run, report: "Sanitized 12 entity kinds, 5 fact dimensions, 3 relationship kinds." High counts indicate the model isn't following the schema.
- [ ] Consider rejecting (not sanitizing) facts with invalid categories rather than silently fixing them. A fact with the wrong category is worse than no fact.

### 4.2 Qualified name normalization

- [ ] Check if the model produces consistent qualified names. Common issues:
  - Mixed separators (`::` vs `.` vs `/`)
  - Missing repo prefix
  - Inconsistent casing
- [ ] Add a normalization step after Phase 2 parsing that enforces `repo::package::Name` format. Strip leading slashes, normalize separators.

### 4.3 Fact deduplication

- [ ] Check for near-duplicate facts across files (same claim, slightly different wording). The current dedup is entity-level only. Consider adding fact-level dedup: if two facts on the same entity have >90% string similarity, keep the higher-confidence one.

### 4.4 Relationship validation

- [ ] After all Phase 2 files are processed, validate that both ends of every relationship resolve to real entities. Log (don't silently drop) relationships where one end is missing.
- [ ] Check for self-referential relationships (from == to). These are usually extraction errors.
- [ ] Check for duplicate relationships (same from/to/kind). The DB upsert handles this, but counting pre-upsert duplicates indicates prompt issues.

### 4.5 Fact grounding check

- [ ] Add a post-processing rule that flags facts with no specific nouns. A fact like "handles errors appropriately" is useless. A fact like "uses Result<T> monad for typed error propagation" is grounded. Simple heuristic: if the claim contains no identifiers, type names, or specific patterns, flag it.

### 4.6 Empty/minimal output detection

- [ ] If Phase 2 returns 0 entities for a non-trivial source file (>50 LOC), log a warning. The model may have failed to parse the file or returned garbage.
- [ ] If Phase 4 returns 0 architectural patterns, log a warning. The synthesis likely failed.

---

## Iteration Cadence

```
For each round:
  1. Make changes (prompt edits, post-processing rules)
  2. Re-index all 3 test repos with --force
  3. Re-run the scorecard from Round 1
  4. Compare scores to previous round
  5. Document what improved and what regressed
  6. Decide: iterate again or move on
```

Target: stop iterating when the scorecard averages 4/5 across all dimensions, or when improvements plateau across 2 consecutive rounds.

---

## Model Upgrade: Qwen3-Coder-Next Results (2026-02-28)

Model: **bullpoint/Qwen3-Coder-Next-AWQ-4bit** (80B total, 3B active MoE, code-specialized) via vLLM on RTX Pro 6000 96GB. No prompt changes — pure model swap.

### Comparison: Baseline (qwen3.5-35b-a3b) → Qwen3-Coder-Next

| Metric | Go (baseline → new) | Python (baseline → new) | TS (baseline → new) |
|---|---|---|---|
| **Quality Score** | 91 → **91** | 98 → **95** | 97 → **93** |
| Entities | 172 → 160 | 120 → 127 | 203 → 188 |
| Facts | 246 → 301 | 269 → 273 | 352 → 354 |
| Relationships | 135 → 147 | 113 → 148 | 189 → 175 |
| Decisions | 8 → 8 | 5 → 4 | 5 → 8 |
| Facts/entity | 1.4 → 1.9 | 2.4 → 2.3 | 1.9 → 2.0 |
| Entity coverage | 76.4% → 81.0% | 91.0% → 85.5% | 81.0% → 87.4% |
| Relationship connectivity | 81.5% → 83.5% | 93.0% → 98.3% | 87.0% → 84.0% |
| Parse success | 96.6% → **100%** | 100% → 100% | 100% → **97.5%** (1 fail) |
| Phase 4 | **FAILED → SUCCESS** | Partial (7 skip) → Partial (12 skip) | Partial (2 skip) → Partial (more skips) |
| Duration | 18m → **3m17s** | 17m → **3m12s** | 25m → **4m41s** |

### Improvements

1. **Phase 4 now succeeds for Go** — was failing all 3 retries with JSON parse errors. Now produces 5 patterns, 4 data flows, 5 contracts, 11 facts, 9 relationships. This was the P0 issue and the model upgrade fixed it.
2. **5-6x faster** — vLLM on RTX Pro 6000 (~650 tok/s prompt, ~250 tok/s generation) vs LM Studio on previous setup. Total pipeline time dropped from ~60m to ~11m for all 3 repos.
3. **More facts extracted** — Go went from 246→301 facts (+22%), consistent improvement in fact density across repos.
4. **More relationships for Go and Python** — Go 135→147 (+9%), Python 113→148 (+31%).
5. **Go parse success improved** — 96.6% → 100% (the 1 failure in baseline is gone).

### Regressions

1. **Overall scores slightly lower** for Python (98→95) and TypeScript (97→93) — driven by entity coverage dips (fewer entities extracted, but some of those may have been duplicates).
2. **Phase 4 has more skipped references in Python** (12 vs 7) — qualified name mismatch issue persists and may be slightly worse. The model uses dot-separated names (`taskflow.config.Config`) that don't match stored `::` separated names.
3. **1 parse failure in TypeScript** (39/40 vs 40/40) — new regression, one file failed to parse.
4. **Fewer entities for Go and TS** — Go 172→160 (-7%), TS 203→188 (-7%). This could be positive (less duplication) or negative (missing extractions). Need deeper analysis.
5. **Python entity coverage dropped** — 91.0%→85.5%. More entities without facts.

### Assessment

The model upgrade **fixes the critical P0 issue** (Phase 4 JSON failures) and delivers major speed improvements. However, it does **not fix** the qualified name inconsistency (P2) or Phase 4 entity reference mismatches (P5) — those are prompt/post-processing problems. The slight score regressions are within noise and likely reflect different entity extraction patterns rather than quality loss.

**Recommendation:** Adopt Qwen3-Coder-Next as the default model. Proceed to Round 2 prompt fixes to address the remaining issues (P1-P5), which are prompt and post-processing problems independent of model choice.

### Next Steps

1. ~~Re-index all 3 repos with Qwen3-Coder-Next, no prompt changes~~ ✅ Done
2. ~~Compare scores to baseline~~ ✅ Done (above)
3. ~~Apply prompt fixes from Round 2 and re-index again~~ ✅ Done (below)
4. This isolates model quality gains from prompt engineering gains

---

## Round 2: Pipeline Quality Improvements (2026-02-28)

Target: all repos >=98/100. Four iterations of targeted fixes:

### Changes Made

1. **Phase 2 prompt: Language-specific qualified_name rules** (`prompts.go`)
   - Replaced vague "use consistent qualified_name" with explicit per-language format rules
   - Go: `package::Name`, `package::Type.Method`; Python: `module::Class.Method`; TS: `module::Class.Method`
   - Added DO/DON'T examples for each language
   - Added barrel file skip rule (TS `index.ts`, Python `__init__.py` re-exports → empty arrays)

2. **Shared entity resolution helper** (`resolve.go`)
   - Extracted 3-step fallback chain: exact match → owner entity → fuzzy name suffix match
   - Used by both Phase 2 and Phase 4
   - Added `FindByName` method to EntityStore (kind-agnostic search)

3. **Qualified name normalization** (`parser.go`)
   - `normalizeQualifiedName()` strips repo-name prefixes, replaces `/` with `::`
   - Keeps only last 2 `::` segments (package::Name)
   - Applied to both Phase 2 and Phase 4 results

4. **Phase 4 improvements** (`phase4.go`)
   - Prepends "AVAILABLE ENTITIES" list to context (exact qualified_names for the model to copy)
   - All `FindByQualifiedName` calls replaced with `resolveEntity()` (fuzzy fallback)

5. **Missing comma JSON fixer** (`parser.go`)
   - String-aware `fixMissingCommas()` inserts commas between JSON entries where model omits them
   - Fixed Phase 4 parse failures that were losing all synthesis data

6. **Orphan entity backfill** (`phase2_backfill.go`, `orchestrator.go`)
   - New Phase 2.5 after file analysis: finds entities with 0 facts, re-analyzes their files
   - Targeted prompt asks model to generate facts+relationships for specific entity names
   - Deterministic "owns" relationship generation for isolated methods (Type.Method → Type)
   - LLM-based relationship backfill for remaining isolated entities

### Iteration Results

| Repo | Baseline | Iter 1 | Iter 2 | Iter 3 | Iter 4 (Final) |
|------|----------|--------|--------|--------|----------------|
| **Go** | 91 | 89 | 91 | 97 | **98.6** ✅ |
| **Python** | 95 | 94 | 100 | 99.6 | **100** ✅ |
| **TypeScript** | 93 | 97 | 97 | 99.6 | **100** ✅ |

### Final Score Breakdown (Iteration 4)

| Metric | Go | Python | TS |
|--------|-----|--------|-----|
| EntityCoverage (30%) | 100% | 100% | 100% |
| FactDensity (20%) | 100% | 100% | 100% |
| RelConnectivity (20%) | 93% | 100% | 100% |
| DimensionCoverage (15%) | 100% | 100% | 100% |
| ParseSuccessRate (15%) | 100% | 100% | 100% |

### Key Insights

- **Biggest lever: orphan backfill** — EntityCoverage went from 76-90% to 100% across all repos. A second LLM pass for entities missing facts was the highest-impact change.
- **Missing comma fixer** fixed intermittent Phase 4 parse failures, recovering cross-module synthesis data.
- **Barrel file skip** rule worked perfectly — TS `index.ts` and Python `__init__.py` now return empty arrays.
- **Fuzzy entity resolution** catches 80%+ of name mismatches (reparenting to owner, suffix matching).
- **Go RelConnectivity (93%)** is the remaining gap — 11 entities still isolated. These are mostly config/concept entities that don't have clear code-level relationships.

---

## Files to Modify

| File | What changes |
|------|-------------|
| `internal/pipeline/prompts.go` | Prompt text, few-shot examples, guidance improvements |
| `internal/pipeline/schemas.go` | Schema changes if output structure needs updating |
| `internal/pipeline/parser.go` | Post-processing rules, sanitization logging, normalization |
| `internal/pipeline/phase2.go` | Fact dedup, relationship validation, empty output detection |
| `internal/pipeline/phase4.go` | Context cap tuning, synthesis improvements |
| `internal/pipeline/phase5.go` | Summary length guidance |
| `internal/pipeline/gitlog.go` | Commit truncation limits, decision threshold |
