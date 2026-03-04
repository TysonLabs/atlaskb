# AtlasKB

**The organizational memory your team and agents deserve.**

AtlasKB is a knowledge graph engine that deeply analyzes your GitHub repositories using LLM inference to extract, normalize, and index the full scope of *what* your systems do, *how* they work, *why* they were built that way, and *when* they evolved — then makes that knowledge instantly retrievable for both human engineers and agentic systems.

## The Problem

Knowledge about your systems lives in the worst possible places:

- In individual engineers' heads
- In PR threads nobody will ever re-read
- In commit messages written at 2am
- In docs that were outdated the day after they were written
- In tribal knowledge that walks out the door when someone leaves

Your agents have it even worse. They get dropped into a codebase with zero context, read a few files, hallucinate the rest, and produce code that works but violates every convention your team has established. Humans spend review cycles teaching agents things the organization already knows.

## The Solution

AtlasKB builds a **living knowledge graph** of your entire engineering organization by:

1. **Deep LLM-powered analysis** of every file, PR, issue, commit, and doc across all your repositories
2. **Normalizing** that knowledge into structured entities, facts, decisions, and relationships
3. **Indexing** everything into a vector store for semantic retrieval
4. **Serving** that knowledge through purpose-built interfaces for humans and agents

The result: a single source of truth that understands your systems the way a senior engineer who's been at the company for years does — except it never forgets, never leaves, and is available to every team member and every agent 24/7.

## How It Works

```
GitHub Repos ──→ Extraction Pipeline ──→ Knowledge Graph ──→ Retrieval Layer
                  (multi-pass LLM)       (entities, facts,    (MCP server,
                                          decisions, edges)    API, chat)
```

### Extraction Pipeline

AtlasKB doesn't do keyword matching or shallow parsing. It uses multi-pass LLM inference to build genuine understanding:

- **Structural Inventory** — classify every file, detect frameworks, map entry points
- **File-Level Analysis** — extract purpose, responsibilities, business rules, invariants per file
- **History Mining** — synthesize decisions and rationale from PRs, issues, and commits
- **Cross-Module Synthesis** — trace data flows, contracts, and architectural boundaries
- **Repo-Level Summary** — distill capabilities, responsibilities, and integration points
- **Cross-Repo Linking** — map how services connect, share data, and depend on each other

### Knowledge Graph

Not a document store. A graph of:

- **Entities** — modules, services, functions, types, concepts
- **Facts** — grounded claims about behavior, constraints, and patterns with provenance
- **Decisions** — the *why* behind design choices, linked to PRs, issues, and ADRs
- **Relationships** — depends on, implements, calls, replaced by, introduced in

Every fact carries provenance (where it came from) and confidence (parsed from code vs. inferred by LLM).

### Retrieval Layer

Two modes, one graph:

**For humans** — natural language Q&A with narrative answers and citations. *"How does authentication work across our services?"*

**For agents** — structured, scoped tools that return precise context mid-task:

| Tool | What it returns |
|---|---|
| `get_conventions` | Coding patterns, error handling, naming, test style |
| `get_module_context` | What a module does, its invariants, what depends on it |
| `get_service_contract` | API surface, auth, error codes, retry policy |
| `get_impact_analysis` | Blast radius of changing an entity |
| `get_decision_context` | Why something is the way it is, rejected alternatives |
| `search_knowledge` | Ranked facts with provenance for open queries |

## Why This Matters

**For your engineers:**
- Onboard onto any repo in minutes, not weeks
- Understand *why* code is the way it is, not just *what* it does
- Find answers across repos without hunting through Slack and PRs
- Make changes with confidence about the blast radius

**For your agents:**
- Ground every action in real organizational knowledge instead of hallucination
- Follow your team's conventions automatically
- Understand service contracts before integrating
- Know what they can't violate before they start changing things

**For your organization:**
- Knowledge survives team changes and turnover
- Consistency across repos and teams
- Faster, higher-quality code review (agents get it right the first time)
- A compounding asset — gets smarter with every PR, review, and correction

## Project Status

AtlasKB is in the design and planning phase. See [docs/](docs/) for detailed design documents.

## Installation (Private Homebrew Tap)

AtlasKB can be installed on macOS through a private Homebrew tap.

End-to-end setup and release steps are documented in [docs/homebrew-private-tap.md](docs/homebrew-private-tap.md).

## Running AtlasKB

First-time setup:

```bash
atlaskb setup
```

`atlaskb configure` and `atlaskb init` are aliases.

Start the combined runtime (dashboard + MCP HTTP endpoint):

```bash
atlaskb
```

This serves:
- Dashboard: `http://localhost:3000`
- MCP endpoint: `http://localhost:3000/mcp`

`atlaskb serve` is equivalent.

`atlaskb mcp` remains available for stdio-only MCP clients.

## License

TBD
