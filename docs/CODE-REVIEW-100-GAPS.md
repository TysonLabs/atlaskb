# Code Review Gap Backlog (100 Items)

Generated from `go tool cover -func=coverage.out` on 2026-03-05.

## Closed In This Iteration

- [x] Fixed PowerShell installer version normalization so `--Version 0.1.0` resolves to `v0.1.0`.
- [x] Fixed shell installer dry-run output to avoid false install success messages.
- [x] Fixed pyproject parsing for extras/markers and poetry version extraction.
- [x] Fixed pubspec parsing so nested YAML keys in dependency blocks are not misread as dependencies.
- [x] Added new tests across parser, graph/cost/confidence, git, llm, embeddings, github, config, models, and cli helper/command logic.
- [x] Added preflight and command-path tests for `internal/cli/index.go`, plus `Execute()` and `runServe()` coverage.
- [x] Added `cmd/atlaskb` main-path coverage via `atlaskb version` execution.
- [x] Added `internal/db` error-path tests to remove DB-layer 0% coverage.
- [x] Added mocked GraphQL tests for `internal/github/fetch.go` (success/error/cancel paths).
- [x] Added pure helper tests for `internal/mcp/server.go` (`NewServer`, `clampMaxResults`, `entityPath`, `toEntitySummary`, `jsonResult`, `errorResult`).

## Remaining Top 100 Function-Level Coverage Gaps

Status key: `open` means not yet covered by tests in this run.

001. [ ] open - `github.com/tgeorge06/atlaskb/internal/cli/ask.go:33:			runAsk					0.0%`
002. [ ] open - `github.com/tgeorge06/atlaskb/internal/cli/link.go:46:			runLink					0.0%`
003. [ ] open - `github.com/tgeorge06/atlaskb/internal/cli/link.go:102:			resolveRepoByName			0.0%`
004. [ ] open - `github.com/tgeorge06/atlaskb/internal/cli/link.go:115:			resolveEntityByPath			0.0%`
005. [ ] open - `github.com/tgeorge06/atlaskb/internal/cli/mcp.go:23:			runMCP					0.0%`
006. [ ] open - `github.com/tgeorge06/atlaskb/internal/cli/repos.go:22:			runRepos				0.0%`
007. [ ] open - `github.com/tgeorge06/atlaskb/internal/cli/retry.go:25:			runRetry				0.0%`
008. [ ] open - `github.com/tgeorge06/atlaskb/internal/cli/setup.go:47:			runSetup				0.0%`
009. [ ] open - `github.com/tgeorge06/atlaskb/internal/cli/setup.go:594:			confirmRetry				0.0%`
010. [ ] open - `github.com/tgeorge06/atlaskb/internal/cli/status.go:36:			runStatus				0.0%`
011. [ ] open - `github.com/tgeorge06/atlaskb/internal/mcp/server.go:71:			RegisterTools				0.0%`
012. [ ] open - `github.com/tgeorge06/atlaskb/internal/mcp/server.go:170:		Run					0.0%`
013. [ ] open - `github.com/tgeorge06/atlaskb/internal/mcp/server.go:181:		batchGetEntities			0.0%`
014. [ ] open - `github.com/tgeorge06/atlaskb/internal/mcp/server.go:197:		resolveRepo				0.0%`
015. [ ] open - `github.com/tgeorge06/atlaskb/internal/mcp/server.go:209:		resolveEntity				0.0%`
016. [ ] open - `github.com/tgeorge06/atlaskb/internal/mcp/server.go:242:		lookupRepoName				0.0%`
017. [ ] open - `github.com/tgeorge06/atlaskb/internal/mcp/server.go:549:		handleSearch				0.0%`
018. [ ] open - `github.com/tgeorge06/atlaskb/internal/mcp/server.go:655:		handleListRepos				0.0%`
019. [ ] open - `github.com/tgeorge06/atlaskb/internal/mcp/server.go:677:		handleGetConventions			0.0%`
020. [ ] open - `github.com/tgeorge06/atlaskb/internal/mcp/server.go:750:		handleGetModuleContext			0.0%`
021. [ ] open - `github.com/tgeorge06/atlaskb/internal/mcp/server.go:845:		handleGetServiceContract		0.0%`
022. [ ] open - `github.com/tgeorge06/atlaskb/internal/mcp/server.go:925:		handleGetImpactAnalysis			0.0%`
023. [ ] open - `github.com/tgeorge06/atlaskb/internal/mcp/server.go:1033:		buildTransitivePaths			0.0%`
024. [ ] open - `github.com/tgeorge06/atlaskb/internal/mcp/server.go:1119:		handleGetDecisionContext		0.0%`
025. [ ] open - `github.com/tgeorge06/atlaskb/internal/mcp/server.go:1165:		handleGetTaskContext			0.0%`
026. [ ] open - `github.com/tgeorge06/atlaskb/internal/mcp/server.go:1546:		handleGetExecutionFlows			0.0%`
027. [ ] open - `github.com/tgeorge06/atlaskb/internal/mcp/server.go:1639:		handleGetFunctionalClusters		0.0%`
028. [ ] open - `github.com/tgeorge06/atlaskb/internal/mcp/server.go:1720:		handleGetRepoOverview			0.0%`
029. [ ] open - `github.com/tgeorge06/atlaskb/internal/mcp/server.go:1798:		handleSearchEntities			0.0%`
030. [ ] open - `github.com/tgeorge06/atlaskb/internal/mcp/server.go:1873:		handleGetEntitySource			0.0%`
031. [ ] open - `github.com/tgeorge06/atlaskb/internal/mcp/server.go:1914:		handleSubmitFactFeedback		0.0%`
032. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/decision.go:18:		Create					0.0%`
033. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/decision.go:43:		LinkEntities				0.0%`
034. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/decision.go:56:		GetByID					0.0%`
035. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/decision.go:78:		DeleteByRepo				0.0%`
036. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/decision.go:86:		ListByEntity				0.0%`
037. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/decision.go:117:		CountByRepo				0.0%`
038. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/decision.go:128:		ListByRepo				0.0%`
039. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/entity.go:18:		Create					0.0%`
040. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/entity.go:34:		Upsert					0.0%`
041. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/entity.go:59:		FindByNameAndKind			0.0%`
042. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/entity.go:80:		FindByName				0.0%`
043. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/entity.go:101:		Update					0.0%`
044. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/entity.go:113:		GetByID					0.0%`
045. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/entity.go:129:		GetByIDs				0.0%`
046. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/entity.go:153:		ListByRepo				0.0%`
047. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/entity.go:174:		ListByRepoAndKind			0.0%`
048. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/entity.go:195:		FindByQualifiedName			0.0%`
049. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/entity.go:211:		ListOrphans				0.0%`
050. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/entity.go:236:		ListWithoutRelationships		0.0%`
051. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/entity.go:261:		CountByRepo				0.0%`
052. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/entity.go:283:		CountWithFacts				0.0%`
053. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/entity.go:294:		CountWithRelationships			0.0%`
054. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/entity.go:305:		FindByPath				0.0%`
055. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/entity.go:322:		FindByPathSuffix			0.0%`
056. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/entity.go:339:		ListByPathSuffix			0.0%`
057. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/entity.go:359:		DeleteByRepo				0.0%`
058. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/entity.go:368:		ListDistinctPaths			0.0%`
059. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/entity.go:393:		SearchByName				0.0%`
060. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/entity.go:456:		SearchFuzzy				0.0%`
061. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/entity.go:503:		NormalizeName				0.0%`
062. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/entity.go:522:		ListByPath				0.0%`
063. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/entity.go:544:		Delete					0.0%`
064. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/entity.go:567:		UpdateSummaryEmbedding			0.0%`
065. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/entity.go:579:		ListByRepoWithoutSummaryEmbedding	0.0%`
066. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/entity.go:602:		SearchBySummaryVector			0.0%`
067. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/entity.go:638:		MaxSummarySimilarity			0.0%`
068. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/entity.go:666:		DeleteByPath				0.0%`
069. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/fact.go:19:		Create					0.0%`
070. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/fact.go:40:		UpdateEmbedding				0.0%`
071. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/fact.go:51:		UpdateConfidence			0.0%`
072. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/fact.go:62:		GetByID					0.0%`
073. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/fact.go:81:		ListByEntity				0.0%`
074. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/fact.go:107:		ListByEntityLimited			0.0%`
075. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/fact.go:132:		ListByRepoWithoutEmbedding		0.0%`
076. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/fact.go:163:		SearchByVector				0.0%`
077. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/fact.go:203:		SearchByVectorForEntity			0.0%`
078. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/fact.go:233:		SearchByKeyword				0.0%`
079. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/fact.go:273:		SearchByFTSRanked			0.0%`
080. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/fact.go:310:		SetSupersededBy				0.0%`
081. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/fact.go:321:		CountByRepo				0.0%`
082. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/fact.go:343:		ListByRepoAndCategory			0.0%`
083. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/fact.go:372:		MaxSimilarityByEntity			0.0%`
084. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/fact.go:400:		ListByRepoAndCategoryAllRepos		0.0%`
085. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/fact.go:427:		DeleteByRepo				0.0%`
086. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/fact_feedback.go:23:	Create					0.0%`
087. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/fact_feedback.go:40:	Resolve					0.0%`
088. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/fact_feedback.go:54:	List					0.0%`
089. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/fact_feedback.go:92:	ListPendingByRepo			0.0%`
090. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/fact_feedback.go:96:	CountPendingByFactIDs			0.0%`
091. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/fact_feedback.go:120:	GetByID					0.0%`
092. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/fact_feedback.go:137:	SubmitFactFeedback			0.0%`
093. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/flow.go:31:		Upsert					0.0%`
094. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/flow.go:53:		DeleteByRepo				0.0%`
095. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/flow.go:62:		ListByRepo				0.0%`
096. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/flow.go:90:		FindByEntity				0.0%`
097. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/indexing_run.go:17:	Create					0.0%`
098. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/indexing_run.go:33:	Complete				0.0%`
099. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/indexing_run.go:64:	GetLatest				0.0%`
100. [ ] open - `github.com/tgeorge06/atlaskb/internal/models/indexing_run.go:95:	ListByRepo				0.0%`
