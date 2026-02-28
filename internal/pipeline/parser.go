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
	fencePattern  = regexp.MustCompile("(?s)```(?:json)?\\s*\n?(.*?)\\s*```")
	singleFence   = regexp.MustCompile("(?s)^`([^`].*?)`$")
	trailingComma = regexp.MustCompile(`,\s*([}\]])`)
	// Match unquoted JSON keys: word characters after { or , followed by :
	unquotedKey   = regexp.MustCompile(`([{,]\s*)([a-zA-Z_][a-zA-Z0-9_]*)\s*:`)
)

// CleanJSON strips markdown code fences and other common LLM output artifacts.
func CleanJSON(raw string) string {
	raw = strings.TrimSpace(raw)

	// Strip markdown code fences (triple backticks)
	if matches := fencePattern.FindStringSubmatch(raw); len(matches) > 1 {
		raw = strings.TrimSpace(matches[1])
	}

	// Strip single-backtick wrapping
	if matches := singleFence.FindStringSubmatch(raw); len(matches) > 1 {
		raw = strings.TrimSpace(matches[1])
	}

	// Remove any remaining stray backticks
	raw = strings.ReplaceAll(raw, "`", "")

	// Strip "Thinking Process:" or similar preamble before JSON
	if idx := strings.Index(raw, "{"); idx > 0 {
		// Check if everything before { is non-JSON preamble
		preamble := strings.TrimSpace(raw[:idx])
		if len(preamble) > 0 && !strings.HasPrefix(preamble, "[") {
			raw = raw[idx:]
		}
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

	// Strip ellipsis placeholders outside of quoted strings
	result = stripEllipsis(result)

	// Fix unquoted JSON keys (common with local models)
	result = unquotedKey.ReplaceAllString(result, `${1}"${2}":`)

	// Fix unquoted string values (common with local models)
	// Match patterns like: "key": word_value (where word_value is not a JSON literal)
	result = fixUnquotedValues(result)

	// Fix trailing commas after cleanup
	result = trailingComma.ReplaceAllString(result, `$1`)

	return result
}

// fixUnquotedValues wraps unquoted string values in JSON with double quotes.
// Handles cases like "key": some_value → "key": "some_value"
// Only operates outside of quoted strings.
var unquotedValuePattern = regexp.MustCompile(`(:\s*)([a-zA-Z_][a-zA-Z0-9_]*(?:\s+[a-zA-Z_][a-zA-Z0-9_]*)*)(\s*[,}\]])`)

func fixUnquotedValues(s string) string {
	jsonLiterals := map[string]bool{"true": true, "false": true, "null": true}

	// Process in a string-aware way
	var out strings.Builder
	out.Grow(len(s))
	inString := false
	escaped := false

	i := 0
	for i < len(s) {
		c := s[i]

		if escaped {
			out.WriteByte(c)
			escaped = false
			i++
			continue
		}
		if c == '\\' && inString {
			out.WriteByte(c)
			escaped = true
			i++
			continue
		}
		if c == '"' {
			inString = !inString
			out.WriteByte(c)
			i++
			continue
		}
		if inString {
			out.WriteByte(c)
			i++
			continue
		}

		// Outside a string, check if we're at a colon followed by an unquoted value
		if c == ':' {
			out.WriteByte(c)
			i++
			// Skip whitespace after colon
			for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
				out.WriteByte(s[i])
				i++
			}
			if i >= len(s) {
				continue
			}
			// Check if next char starts an unquoted identifier (not a JSON value start)
			next := s[i]
			if next != '"' && next != '{' && next != '[' && next != '-' && !(next >= '0' && next <= '9') {
				// Collect the unquoted value
				start := i
				for i < len(s) && s[i] != ',' && s[i] != '}' && s[i] != ']' && s[i] != '\n' {
					i++
				}
				val := strings.TrimSpace(s[start:i])
				if !jsonLiterals[val] && len(val) > 0 {
					out.WriteByte('"')
					out.WriteString(val)
					out.WriteByte('"')
				} else {
					out.WriteString(val)
				}
				continue
			}
			continue
		}

		out.WriteByte(c)
		i++
	}

	return out.String()
}

// stripEllipsis removes all sequences of 2+ dots that appear outside quoted strings,
// replacing them with null (if after a colon) or removing them (if in arrays/elsewhere).
// This handles all the various ways local LLMs use "..." as placeholder values.
func stripEllipsis(s string) string {
	var out strings.Builder
	out.Grow(len(s))

	inString := false
	escaped := false

	i := 0
	for i < len(s) {
		c := s[i]

		if escaped {
			out.WriteByte(c)
			escaped = false
			i++
			continue
		}

		if c == '\\' && inString {
			out.WriteByte(c)
			escaped = true
			i++
			continue
		}

		if c == '"' {
			inString = !inString
			out.WriteByte(c)
			i++
			continue
		}

		if inString {
			out.WriteByte(c)
			i++
			continue
		}

		// Outside a string: check for sequences of 2+ dots
		if c == '.' && i+1 < len(s) && s[i+1] == '.' {
			// Count consecutive dots
			dotEnd := i
			for dotEnd < len(s) && s[dotEnd] == '.' {
				dotEnd++
			}
			// Skip the dots entirely — look back to see if we need to insert "null"
			// Find the last non-whitespace character before these dots
			needNull := false
			for j := out.Len() - 1; j >= 0; j-- {
				ch := out.String()[j]
				if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
					continue
				}
				if ch == ':' {
					needNull = true
				}
				break
			}
			if needNull {
				out.WriteString("null")
			}
			// Skip any trailing whitespace after the dots too
			i = dotEnd
			continue
		}

		out.WriteByte(c)
		i++
	}

	return out.String()
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
