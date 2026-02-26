# Vision

## AtlasKB exists because organizations forget faster than they learn.

Every day, your engineering team generates knowledge. A developer chooses PostgreSQL over DynamoDB for a new service and explains why in a PR description. A tech lead refactors an auth flow and documents the tradeoffs in an ADR. A senior engineer fixes a subtle race condition and leaves a comment explaining the invariant that must hold.

This knowledge is generated once, consumed a few times, and then effectively lost — buried in a git history that nobody will search, a PR thread that nobody will re-read, a Slack message that scrolled off the screen months ago.

Meanwhile, every new engineer, every new agent, every new task starts from near-zero context. The same questions get asked. The same mistakes get made. The same decisions get re-litigated because nobody remembers why the last decision was made.

## The insight

The knowledge already exists. It's embedded in your code, your commits, your pull requests, your issues, your docs. It's just **fragmented, implicit, and inaccessible**.

What's missing isn't more documentation — it's a system that can:

1. **Extract** knowledge from where it naturally lives (code, PRs, commits, issues)
2. **Synthesize** it into a coherent understanding (not just indexing — actual comprehension)
3. **Normalize** it into a structured, queryable form (entities, facts, decisions, relationships)
4. **Serve** it to whoever needs it, in the format they need it (narrative for humans, structured for agents)

## The bet

AtlasKB bets that LLMs are now good enough to do what only humans could do before: read code and surrounding context, understand intent, and articulate knowledge in a way that's useful to others.

This isn't cheap. Deeply analyzing a large repo might burn millions of tokens. But the alternative — engineers spending hours onboarding, agents producing context-free code, organizations re-learning what they already knew — is far more expensive.

The cost of extraction is paid once per repo (with incremental updates). The value of retrieval is paid forward to every engineer, every agent, every task, forever.

## The end state

When AtlasKB is mature, it should feel like your organization has a **collective memory** — a senior engineer who has read every line of code, every PR, every issue, across every repo, and can answer any question about how your systems work and why.

No single human can hold all that context. AtlasKB can.

## Principles

### Depth over speed
We'd rather take hours to deeply understand a repository than minutes to shallowly index it. Surface-level extraction produces surface-level answers. We want the kind of understanding that lets you answer "what would break if I changed this?" — and that requires genuine comprehension.

### Provenance is non-negotiable
Every fact in the knowledge graph must be traceable to its source — a file, a line number, a commit, a PR, an issue. Knowledge without provenance is just an opinion. Engineers and agents need to verify, and the system needs to earn trust through transparency.

### Agents are first-class citizens
This is not a search engine with an API bolted on. Agent retrieval is a primary use case from day one. The schema, the query interface, the response format — all designed for machines to consume mid-task, not just humans to browse.

### The graph is alive
Repositories change. Teams evolve. Decisions get revisited. AtlasKB must handle incremental updates, not just one-time snapshots. The knowledge graph should reflect the current state of your systems, with history preserved but not presented as current truth.

### Feedback makes it smarter
When a human corrects an agent's work, that correction is knowledge. When a PR review says "we don't do it that way," that's a convention being articulated. AtlasKB should learn from these signals, creating a virtuous cycle where usage improves the knowledge base.
