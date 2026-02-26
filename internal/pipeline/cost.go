package pipeline

import "fmt"

// Token cost estimates per million tokens (USD)
const (
	SonnetInputPer1M  = 3.00
	SonnetOutputPer1M = 15.00
	OpusInputPer1M    = 15.00
	OpusOutputPer1M   = 75.00
)

// Rough bytes-to-tokens ratio for code
const bytesPerToken = 4

type CostEstimate struct {
	Phase2Tokens    int     `json:"phase2_tokens"`
	Phase2CostUSD   float64 `json:"phase2_cost_usd"`
	Phase4Tokens    int     `json:"phase4_tokens"`
	Phase4CostUSD   float64 `json:"phase4_cost_usd"`
	Phase5Tokens    int     `json:"phase5_tokens"`
	Phase5CostUSD   float64 `json:"phase5_cost_usd"`
	TotalInputTokens int   `json:"total_input_tokens"`
	TotalCostUSD    float64 `json:"total_cost_usd"`
}

func EstimateCost(m *Manifest) CostEstimate {
	est := CostEstimate{}

	// Phase 2: analyze each file. Input = file content + prompt overhead (~500 tokens)
	// Output estimate: ~1000 tokens per file
	promptOverhead := 500
	outputPerFile := 1000

	for _, fi := range m.Files {
		if !ShouldAnalyze(fi) {
			continue
		}
		inputTokens := int(fi.Size)/bytesPerToken + promptOverhead
		est.Phase2Tokens += inputTokens + outputPerFile
	}
	est.Phase2CostUSD = float64(est.Phase2Tokens) / 1_000_000 * (SonnetInputPer1M + SonnetOutputPer1M) / 2

	// Phase 4: synthesis. Roughly 10% of phase 2 input, using Opus
	est.Phase4Tokens = est.Phase2Tokens / 10
	est.Phase4CostUSD = float64(est.Phase4Tokens) / 1_000_000 * (OpusInputPer1M + OpusOutputPer1M) / 2

	// Phase 5: summary. Small fixed cost
	est.Phase5Tokens = 5000
	est.Phase5CostUSD = float64(est.Phase5Tokens) / 1_000_000 * (OpusInputPer1M + OpusOutputPer1M) / 2

	est.TotalInputTokens = est.Phase2Tokens + est.Phase4Tokens + est.Phase5Tokens
	est.TotalCostUSD = est.Phase2CostUSD + est.Phase4CostUSD + est.Phase5CostUSD

	return est
}

func FormatCost(est CostEstimate) string {
	return fmt.Sprintf(
		"Estimated cost:\n"+
			"  Phase 2 (file analysis):     ~%d tokens  ~$%.2f\n"+
			"  Phase 4 (synthesis):         ~%d tokens  ~$%.2f\n"+
			"  Phase 5 (summary):           ~%d tokens  ~$%.2f\n"+
			"  Total:                       ~%d tokens  ~$%.2f",
		est.Phase2Tokens, est.Phase2CostUSD,
		est.Phase4Tokens, est.Phase4CostUSD,
		est.Phase5Tokens, est.Phase5CostUSD,
		est.TotalInputTokens, est.TotalCostUSD,
	)
}
