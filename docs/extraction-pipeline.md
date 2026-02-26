# Extraction Pipeline

This document describes how AtlasKB transforms raw repository data into structured knowledge.

## Philosophy

The extraction pipeline is unapologetically LLM-heavy. We don't use LLMs as a shortcut — we use them because the task genuinely requires language understanding. Parsing an AST tells you what functions exist. Only an LLM can tell you *what a function is for* and *why it was written that way*.

We accept the cost (time, tokens) in exchange for depth. A shallow index is cheap to build and nearly useless to query. A deep knowledge graph is expensive to build and transformatively useful.

## Pipeline Overview

```
 ┌─────────────────┐
 │  GitHub Clone    │
 └────────┬────────┘
          ▼
 ┌─────────────────┐
 │  Phase 1:       │  Classify files, detect stack, map structure
 │  Inventory      │  (lightweight, mostly heuristic)
 └────────┬────────┘
          ▼
 ┌─────────────────┐
 │  Phase 2:       │  Deep LLM analysis per file
 │  File Analysis  │  (parallelizable, bulk of token spend)
 └────────┬────────┘
          ▼
 ┌─────────────────┐
 │  Phase 3:       │  Analyze PRs, issues, significant commits
 │  History Mining │  (parallelizable, medium token spend)
 └────────┬────────┘
          ▼
 ┌─────────────────┐
 │  Phase 4:       │  Cross-file relationships, data flows,
 │  Synthesis      │  architectural patterns
 └────────┬────────┘
          ▼
 ┌─────────────────┐
 │  Phase 5:       │  High-level repo summary,
 │  Repo Summary   │  capability inventory
 └────────┬────────┘
          ▼
 ┌─────────────────┐
 │  Phase 6:       │  Cross-repo links, shared concepts,
 │  Cross-Repo     │  service mesh mapping
 └─────────────────┘
```

## Phase 1: Structural Inventory

**Goal:** Build a manifest of the repository before any LLM calls. Understand what we're looking at.

**Inputs:** Cloned repository

**Process:**
1. Walk the file tree, classify each file:
   - Source code (by language)
   - Tests
   - Configuration (app config, CI/CD, infra)
   - Documentation (READMEs, docs/, ADRs, wikis)
   - Build/dependency (package.json, go.mod, Cargo.toml, etc.)
   - Generated/vendored (detect and skip)
2. Detect the technology stack:
   - Languages, frameworks, build tools
   - Database/queue/cache integrations (from config and dependencies)
   - External service integrations (from config, env vars, imports)
3. Identify structural landmarks:
   - Entry points (main, cmd, handlers, routes)
   - Module/package boundaries
   - Test structure and coverage patterns
4. Estimate analysis scope:
   - File count, total lines, complexity heuristics
   - Token budget estimation for subsequent phases

**Outputs:**
- Repository manifest (file classifications, stack detection, landmarks)
- Analysis plan (which files to analyze in what order, estimated cost)

**LLM usage:** None. This phase is pure heuristics and file inspection.

## Phase 2: File-Level Analysis

**Goal:** Extract deep knowledge from every meaningful source file.

**Inputs:** Repository manifest, individual source files

**Process:**

For each file (parallelizable across files):
1. Provide the LLM with:
   - The file contents
   - Its path and classification from Phase 1
   - Surrounding context (imports, parent module purpose if known)
   - The repo's detected stack/framework context
2. Ask structured questions:
   - What is the responsibility of this file/module?
   - What entities (types, functions, endpoints) does it define?
   - For each entity: what does it do, what does it assume, what are its invariants?
   - What business rules or domain logic does it encode?
   - What patterns/conventions does it follow?
   - What dependencies does it have (imports, calls, data access)?
   - Is there anything notable, unusual, or risky?
3. Parse the LLM response into Entity and Fact nodes

**Outputs:**
- Entity nodes for each significant code element
- Fact nodes for each extracted claim
- Preliminary Relationship edges (imports, calls)

**LLM usage:** Heavy. One call per file (or per logical group for small files). This is the bulk of the token budget.

**Optimization:** Files can be batched by similarity. Test files can reference their implementation counterparts. Generated/vendored files are skipped entirely.

## Phase 3: History Mining

**Goal:** Extract the "why" layer from git history and GitHub artifacts.

**Inputs:** Git log, GitHub PRs, issues, and comments (via API)

**Process:**

1. **Filter for signal:** Not every commit matters. Filter for:
   - PRs with substantial descriptions or review discussions
   - Commits with meaningful messages (skip "fix typo", "wip", merge commits)
   - Issues that describe design decisions, bugs with root cause analysis, or feature rationale
   - ADRs and design docs (if they exist)

2. **For each significant PR/issue** (parallelizable):
   - Provide the LLM with the PR description, diff summary, and review comments
   - Ask: What decision was made? Why? What alternatives were discussed? What tradeoffs?
   - Ask: What entities (from Phase 2) does this decision affect?
   - Parse into Decision nodes and link to affected Entities

3. **For significant commits without PRs:**
   - Provide the commit message and diff summary
   - Extract any stated reasoning or context
   - Link to affected files/entities

**Outputs:**
- Decision nodes with rationale and alternatives
- Additional Fact nodes (especially "why" facts)
- Temporal metadata on entities (when introduced, when last changed)

**LLM usage:** Moderate. Filtered to significant artifacts only.

## Phase 4: Cross-Module Synthesis

**Goal:** Understand how pieces fit together. Trace data flows, identify architectural boundaries, map contracts.

**Inputs:** All Entity, Fact, and Relationship nodes from Phases 2-3

**Process:**

1. **Group entities by domain/module** using directory structure and import graphs
2. **For each module group,** provide the LLM with:
   - Summaries of all entities in the group
   - Import/call relationships crossing the group boundary
   - Any decisions that affected the group
3. **Ask the LLM to synthesize:**
   - How do these modules interact? What data flows between them?
   - What are the contracts (explicit or implicit) between modules?
   - What are the architectural boundaries? What pattern does this follow?
   - Where are the coupling points? What's tightly vs. loosely coupled?
4. **For services with external integrations:**
   - Map the integration pattern (REST calls, queue consumers, DB access)
   - Document the expected contract (what it sends, what it expects back)

**Outputs:**
- Refined Relationship edges (with descriptions and strength)
- Architecture-level Fact nodes (patterns, boundaries, data flows)
- Contract descriptions for inter-module and inter-service boundaries

**LLM usage:** Moderate. Fewer calls but with larger context windows (synthesizing across modules).

## Phase 5: Repository-Level Summary

**Goal:** Distill everything into a high-level understanding of the repository.

**Inputs:** All knowledge from Phases 1-4

**Process:**

1. Provide the LLM with:
   - Entity summaries (grouped by module)
   - Key architectural facts
   - Major decisions
   - Relationship graph summary
2. Ask for a structured repo summary:
   - What is this repository? (one paragraph)
   - What are its primary responsibilities/capabilities?
   - What is its architecture at a high level?
   - What are its key integration points (what it depends on, what depends on it)?
   - What are the known risks, tech debt, or areas of concern?
   - What conventions and patterns does this codebase follow?

**Outputs:**
- Repo-level Entity node with comprehensive summary
- Capability inventory (structured list of what this repo can do)
- Convention guide (how to write code that fits this repo)

**LLM usage:** Light. One or two calls with synthesized input.

## Phase 6: Cross-Repository Linking

**Goal:** After multiple repos are indexed, understand how they relate.

**Inputs:** Repo-level summaries and entity graphs from all indexed repos

**Process:**

1. Identify potential connections:
   - Shared entity names or concepts across repos
   - Service-to-service calls (from integration patterns detected in Phase 4)
   - Shared data models or database references
   - Common dependencies or frameworks
2. For each potential connection, ask the LLM:
   - Is this a real relationship or a name collision?
   - What is the nature of the relationship?
   - What is the contract between these repos?
3. Build cross-repo edges and shared concept nodes

**Outputs:**
- Cross-repo Relationship edges
- Shared concept Entity nodes (concepts that span repos)
- Organization-level architecture map

**LLM usage:** Light to moderate, depending on number of repos and connections.

---

## Incremental Updates

After initial indexing, AtlasKB should not re-analyze the entire repo on every change. Instead:

1. **Watch for changes** (webhook or polling on new commits/PRs)
2. **Identify affected files** from the diff
3. **Re-run Phase 2** on changed files only
4. **Re-run Phase 3** on new PRs/issues
5. **Selectively re-run Phase 4** for modules containing changed files
6. **Re-run Phase 5** if significant changes detected
7. **Mark potentially stale facts** on entities adjacent to changes

The incremental pipeline should be fast enough to run on every merge to main, keeping the knowledge graph near-real-time.

## Cost Management

Token spend is dominated by Phase 2 (file-level analysis). Strategies to manage cost:

- **Skip generated/vendored files** entirely
- **Batch small files** into single LLM calls
- **Cache unchanged files** across re-indexing runs
- **Tiered analysis depth:** critical modules get deeper analysis, utility code gets lighter treatment
- **Model selection:** use capable but cost-efficient models for bulk extraction, reserve frontier models for synthesis phases
