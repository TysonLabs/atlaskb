# TODO: Configurable Directory Exclusions

## Problem

Some repos contain large vendored/third-party directories that should not be indexed. For example, `vector-transcribe-ui/docroot/plugins` contains third-party PHP plugins that pollute the knowledge base with irrelevant entities, facts, and relationships.

Currently the pipeline only excludes hardcoded patterns (node_modules, vendor, dist, build, .git, etc.) via ctags flags and the file classifier. There's no way for users to specify per-repo exclusions.

## Examples of directories to exclude

- `vector-transcribe-ui/docroot/plugins` -- third-party PHP plugins
- `vector-transcribe-ui/docroot/vendor` -- composer dependencies
- Any `third_party/`, `external/`, `assets/` directories with vendored code

## Proposed Solution

Add an `--exclude` flag to `atlaskb index` that accepts glob patterns:

```bash
atlaskb index --exclude "docroot/plugins/**" --exclude "docroot/vendor/**" /path/to/repo
```

### Implementation

1. **CLI flag**: Add `--exclude` string slice flag to `index.go`
2. **Pass to orchestrator**: Add `Excludes []string` to `OrchestratorConfig`
3. **Phase 1 filtering**: Apply exclude patterns in `BuildManifest()` / `ClassifyFile()` before files enter the manifest
4. **Ctags filtering**: Pass exclude patterns as additional `--exclude` flags to ctags
5. **Persist per-repo**: Optionally store exclude patterns in the `repos` table so incremental re-indexes remember them

### Alternative: `.atlaskbignore` file

Support a `.atlaskbignore` file in the repo root (gitignore syntax):

```
docroot/plugins/
docroot/vendor/
*.min.js
*.min.css
```

This would be checked into the repo and automatically picked up by the pipeline.

Both approaches could coexist: CLI flags for one-off overrides, `.atlaskbignore` for persistent per-repo config.
