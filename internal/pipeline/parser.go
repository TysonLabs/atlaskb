package pipeline

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/tgeorge06/atlaskb/internal/models"
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

var (
	fencePattern    = regexp.MustCompile("(?s)```(?:json)?\\s*\n?(.*?)\\s*```")
	// Matches { ... } or [ ... ] used as placeholder objects/arrays
	placeholderObj  = regexp.MustCompile(`\{\s*\.\.\.\s*\}`)
	placeholderArr  = regexp.MustCompile(`\[\s*\.\.\.\s*\]`)
	// Matches "..." used as a placeholder string value (but not inside a real string)
	placeholderStr  = regexp.MustCompile(`"\.{2,}"`)
	// Matches trailing commas before ] or }
	trailingComma   = regexp.MustCompile(`,\s*([}\]])`)
)

// CleanJSON strips markdown code fences and other common LLM output artifacts.
func CleanJSON(raw string) string {
	raw = strings.TrimSpace(raw)

	// Strip markdown code fences
	if matches := fencePattern.FindStringSubmatch(raw); len(matches) > 1 {
		raw = strings.TrimSpace(matches[1])
	}

	// Find the first { or [ to start the JSON
	start := strings.IndexAny(raw, "{[")
	if start < 0 {
		return raw
	}

	opener := raw[start]
	var closer byte = '}'
	if opener == '[' {
		closer = ']'
	}

	// Walk forward to find the balanced closing bracket
	depth := 0
	inString := false
	escaped := false
	end := -1
	for i := start; i < len(raw); i++ {
		c := raw[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' && inString {
			escaped = true
			continue
		}
		if c == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		if c == opener || (opener == '{' && c == '{') || (opener == '[' && c == '[') {
			if c == '{' || c == '[' {
				depth++
			}
		}
		if c == closer || (closer == '}' && c == '}') || (closer == ']' && c == ']') {
			if c == '}' || c == ']' {
				depth--
			}
		}
		if depth == 0 {
			end = i
			break
		}
	}

	if end < 0 {
		// Fallback: use LastIndex approach
		end = strings.LastIndexByte(raw, closer)
		if end < 0 {
			return raw[start:]
		}
	}

	result := raw[start : end+1]

	// Clean up LLM placeholder artifacts
	result = placeholderObj.ReplaceAllString(result, `null`)
	result = placeholderArr.ReplaceAllString(result, `[]`)
	result = placeholderStr.ReplaceAllString(result, `""`)
	result = trailingComma.ReplaceAllString(result, `$1`)

	return result
}

// Valid enum values for sanitization.
var (
	validEntityKinds = map[string]bool{
		models.EntityModule: true, models.EntityService: true, models.EntityFunction: true,
		models.EntityType: true, models.EntityEndpoint: true, models.EntityConcept: true,
		models.EntityConfig: true,
	}
	validFactDimensions = map[string]bool{
		models.DimensionWhat: true, models.DimensionHow: true,
		models.DimensionWhy: true, models.DimensionWhen: true,
	}
	validFactCategories = map[string]bool{
		models.CategoryBehavior: true, models.CategoryConstraint: true,
		models.CategoryPattern: true, models.CategoryConvention: true,
		models.CategoryDebt: true, models.CategoryRisk: true,
	}
	validConfidenceLevels = map[string]bool{
		models.ConfidenceHigh: true, models.ConfidenceMedium: true, models.ConfidenceLow: true,
	}
	validRelKinds = map[string]bool{
		models.RelDependsOn: true, models.RelCalls: true, models.RelImplements: true,
		models.RelExtends: true, models.RelProduces: true, models.RelConsumes: true,
		models.RelReplacedBy: true, models.RelTestedBy: true, models.RelConfiguredBy: true,
		models.RelOwns: true,
	}
	validRelStrengths = map[string]bool{
		models.StrengthStrong: true, models.StrengthModerate: true, models.StrengthWeak: true,
	}
)

func sanitizeOrDefault(val string, valid map[string]bool, fallback string) string {
	val = strings.ToLower(strings.TrimSpace(val))
	if valid[val] {
		return val
	}
	return fallback
}

func sanitizeEntities(entities []ExtractedEntity) []ExtractedEntity {
	var out []ExtractedEntity
	for _, e := range entities {
		e.Kind = sanitizeOrDefault(e.Kind, validEntityKinds, models.EntityConcept)
		out = append(out, e)
	}
	return out
}

func sanitizeFacts(facts []ExtractedFact) []ExtractedFact {
	var out []ExtractedFact
	for _, f := range facts {
		f.Dimension = sanitizeOrDefault(f.Dimension, validFactDimensions, models.DimensionWhat)
		f.Category = sanitizeOrDefault(f.Category, validFactCategories, models.CategoryBehavior)
		f.Confidence = sanitizeOrDefault(f.Confidence, validConfidenceLevels, models.ConfidenceMedium)
		out = append(out, f)
	}
	return out
}

func sanitizeRelationships(rels []ExtractedRelation) []ExtractedRelation {
	var out []ExtractedRelation
	for _, r := range rels {
		r.Kind = sanitizeOrDefault(r.Kind, validRelKinds, models.RelDependsOn)
		r.Strength = sanitizeOrDefault(r.Strength, validRelStrengths, models.StrengthModerate)
		out = append(out, r)
	}
	return out
}

func ParsePhase2(raw string) (*Phase2Result, error) {
	cleaned := CleanJSON(raw)
	var result Phase2Result
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, err
	}
	result.Entities = sanitizeEntities(result.Entities)
	result.Facts = sanitizeFacts(result.Facts)
	result.Relationships = sanitizeRelationships(result.Relationships)
	return &result, nil
}

func ParsePhase4(raw string) (*Phase4Result, error) {
	cleaned := CleanJSON(raw)
	var result Phase4Result
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, err
	}
	result.Facts = sanitizeFacts(result.Facts)
	result.Relationships = sanitizeRelationships(result.Relationships)
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
	result.Facts = sanitizeFacts(result.Facts)
	return &result, nil
}
