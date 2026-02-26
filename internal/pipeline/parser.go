package pipeline

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Phase2Result is the parsed output from a phase 2 LLM call.
type Phase2Result struct {
	FileSummary   string              `json:"file_summary"`
	Entities      []ExtractedEntity   `json:"entities"`
	Facts         []ExtractedFact     `json:"facts"`
	Relationships []ExtractedRelation `json:"relationships"`
}

type ExtractedEntity struct {
	Kind          string   `json:"kind"`
	Name          string   `json:"name"`
	QualifiedName string   `json:"qualified_name"`
	Summary       string   `json:"summary"`
	Capabilities  []string `json:"capabilities"`
	Assumptions   []string `json:"assumptions"`
}

type ExtractedFact struct {
	EntityName string `json:"entity_name"`
	Claim      string `json:"claim"`
	Dimension  string `json:"dimension"`
	Category   string `json:"category"`
	Confidence string `json:"confidence"`
}

type ExtractedRelation struct {
	From        string `json:"from"`
	To          string `json:"to"`
	Kind        string `json:"kind"`
	Description string `json:"description"`
	Strength    string `json:"strength"`
}

// Phase4Result is the parsed output from a phase 4 LLM call.
type Phase4Result struct {
	ArchitecturalPatterns []struct {
		Pattern     string `json:"pattern"`
		Description string `json:"description"`
		Confidence  string `json:"confidence"`
	} `json:"architectural_patterns"`
	DataFlows []struct {
		Description string `json:"description"`
		FromModule  string `json:"from_module"`
		ToModule    string `json:"to_module"`
		Mechanism   string `json:"mechanism"`
	} `json:"data_flows"`
	Contracts []struct {
		Between     []string `json:"between"`
		Description string   `json:"description"`
		Explicit    bool     `json:"explicit"`
	} `json:"contracts"`
	Facts         []ExtractedFact     `json:"facts"`
	Relationships []ExtractedRelation `json:"relationships"`
}

// Phase5Result is the parsed output from a phase 5 LLM call.
type Phase5Result struct {
	Summary      string `json:"summary"`
	Capabilities []string `json:"capabilities"`
	Architecture string `json:"architecture"`
	Conventions  []struct {
		Category    string   `json:"category"`
		Description string   `json:"description"`
		Examples    []string `json:"examples"`
	} `json:"conventions"`
	RisksAndDebt         []string `json:"risks_and_debt"`
	KeyIntegrationPoints []string `json:"key_integration_points"`
}

// GitLogResult is the parsed output from git log analysis.
type GitLogResult struct {
	Facts     []ExtractedFact `json:"facts"`
	Decisions []struct {
		Summary     string `json:"summary"`
		Description string `json:"description"`
		Rationale   string `json:"rationale"`
		MadeAt      string `json:"made_at"`
	} `json:"decisions"`
}

var fencePattern = regexp.MustCompile("(?s)```(?:json)?\\s*\n?(.*?)\\s*```")

// CleanJSON strips markdown code fences and other common LLM output artifacts.
func CleanJSON(raw string) string {
	raw = strings.TrimSpace(raw)

	// Strip markdown code fences
	if matches := fencePattern.FindStringSubmatch(raw); len(matches) > 1 {
		raw = matches[1]
	}

	// Strip leading/trailing non-JSON characters
	start := strings.IndexAny(raw, "{[")
	if start < 0 {
		return raw
	}

	// Find matching closing bracket
	end := strings.LastIndexAny(raw, "}]")
	if end < 0 {
		return raw
	}

	return raw[start : end+1]
}

func ParsePhase2(raw string) (*Phase2Result, error) {
	cleaned := CleanJSON(raw)
	var result Phase2Result
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func ParsePhase4(raw string) (*Phase4Result, error) {
	cleaned := CleanJSON(raw)
	var result Phase4Result
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func ParsePhase5(raw string) (*Phase5Result, error) {
	cleaned := CleanJSON(raw)
	var result Phase5Result
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func ParseGitLog(raw string) (*GitLogResult, error) {
	cleaned := CleanJSON(raw)
	var result GitLogResult
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, err
	}
	return &result, nil
}
