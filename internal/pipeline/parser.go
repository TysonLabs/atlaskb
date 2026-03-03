package pipeline

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/kaptinlin/jsonrepair"
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
	Summary      string   `json:"summary"`
	Capabilities []string `json:"capabilities"`
	Architecture string   `json:"architecture"`
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

// CleanJSON repairs malformed JSON from LLM output using jsonrepair (a port of
// the battle-tested JavaScript jsonrepair library). It handles missing commas,
// unquoted keys, trailing commas, truncated output, markdown fences, and more.
func CleanJSON(raw string) string {
	raw = strings.TrimSpace(raw)

	// Strip markdown code fences before repair.
	if matches := fencePattern.FindStringSubmatch(raw); len(matches) > 1 {
		raw = strings.TrimSpace(matches[1])
	}

	// Strip preamble text before JSON (e.g. "Thinking Process: ...")
	// Don't strip if the raw input looks like a JSON array (starts with '[{' or '[ {').
	if idx := strings.Index(raw, "{"); idx > 0 {
		prefix := strings.TrimSpace(raw[:idx])
		looksLikeArray := len(prefix) == 1 && prefix[0] == '['
		if !looksLikeArray {
			raw = raw[idx:]
		}
	}

	// jsonrepair handles missing commas, unquoted keys, trailing commas,
	// single quotes, truncated JSON, Python constants, and more.
	if repaired, err := jsonrepair.Repair(raw); err == nil {
		return repaired
	}

	// If jsonrepair fails entirely, return the stripped input as-is so the
	// caller's json.Unmarshal produces a descriptive error.
	return raw
}

// --- Domain-specific sanitization (not JSON repair) ---

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

// normalizeQualifiedName cleans up a qualified_name produced by the LLM:
//   - Strip repo-name prefixes (e.g. "atlaskb-typescript-test-repo::src::channels::HttpChannel" → "channels::HttpChannel")
//   - Replace "/" with "::" in package paths
//   - Collapse multiple "::" segments to just package::Name
//   - Ensure "." is only used between type and method
func normalizeQualifiedName(qn string) string {
	// Replace "/" with "::"
	qn = strings.ReplaceAll(qn, "/", "::")

	// Split on "::" to analyze segments
	parts := strings.Split(qn, "::")

	// Remove empty segments
	var cleaned []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			cleaned = append(cleaned, p)
		}
	}
	if len(cleaned) == 0 {
		return qn
	}

	// Keep only the last 2 segments: the package and the symbol
	if len(cleaned) > 2 {
		cleaned = cleaned[len(cleaned)-2:]
	}

	return strings.Join(cleaned, "::")
}

// normalizeQualifiedNames normalizes all qualified_names in a Phase2Result.
func normalizeQualifiedNames(result *Phase2Result) {
	nameMap := make(map[string]string)

	for i := range result.Entities {
		old := result.Entities[i].QualifiedName
		normalized := normalizeQualifiedName(old)
		if normalized != old {
			nameMap[old] = normalized
		}
		result.Entities[i].QualifiedName = normalized
	}

	for i := range result.Facts {
		if newName, ok := nameMap[result.Facts[i].EntityName]; ok {
			result.Facts[i].EntityName = newName
		} else {
			result.Facts[i].EntityName = normalizeQualifiedName(result.Facts[i].EntityName)
		}
	}

	for i := range result.Relationships {
		if newName, ok := nameMap[result.Relationships[i].From]; ok {
			result.Relationships[i].From = newName
		} else {
			result.Relationships[i].From = normalizeQualifiedName(result.Relationships[i].From)
		}
		if newName, ok := nameMap[result.Relationships[i].To]; ok {
			result.Relationships[i].To = newName
		} else {
			result.Relationships[i].To = normalizeQualifiedName(result.Relationships[i].To)
		}
	}
}

// --- Parse functions ---

func ParsePhase2(raw string) (*Phase2Result, error) {
	cleaned := CleanJSON(raw)
	var result Phase2Result
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, err
	}
	result.Entities = sanitizeEntities(result.Entities)
	result.Facts = sanitizeFacts(result.Facts)
	result.Relationships = sanitizeRelationships(result.Relationships)
	normalizeQualifiedNames(&result)
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
	for i := range result.Facts {
		result.Facts[i].EntityName = normalizeQualifiedName(result.Facts[i].EntityName)
	}
	for i := range result.Relationships {
		result.Relationships[i].From = normalizeQualifiedName(result.Relationships[i].From)
		result.Relationships[i].To = normalizeQualifiedName(result.Relationships[i].To)
	}
	for i := range result.DataFlows {
		result.DataFlows[i].FromModule = normalizeQualifiedName(result.DataFlows[i].FromModule)
		result.DataFlows[i].ToModule = normalizeQualifiedName(result.DataFlows[i].ToModule)
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

type Phase3Result struct {
	Facts     []ExtractedFact `json:"facts"`
	Decisions []struct {
		Summary      string `json:"summary"`
		Description  string `json:"description"`
		Rationale    string `json:"rationale"`
		Alternatives []struct {
			Description     string `json:"description"`
			RejectedBecause string `json:"rejected_because"`
		} `json:"alternatives"`
		Tradeoffs []string `json:"tradeoffs"`
		PRNumber  int      `json:"pr_number"`
		MadeAt    string   `json:"made_at"`
	} `json:"decisions"`
}

func ParsePhase3(raw string) (*Phase3Result, error) {
	cleaned := CleanJSON(raw)
	var result Phase3Result
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, err
	}
	result.Facts = sanitizeFacts(result.Facts)
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
