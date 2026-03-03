package pipeline

import (
	"encoding/json"

	"github.com/tgeorge06/atlaskb/internal/llm"
)

var SchemaPhase2 = &llm.JSONSchema{
	Name: "phase2_extraction",
	Schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "file_summary": { "type": "string" },
    "entities": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "kind": { "type": "string", "enum": ["module","service","function","type","endpoint","concept","config"] },
          "name": { "type": "string" },
          "qualified_name": { "type": "string" },
          "summary": { "type": "string" },
          "capabilities": { "type": "array", "items": { "type": "string" } },
          "assumptions": { "type": "array", "items": { "type": "string" } }
        },
        "required": ["kind","name","qualified_name","summary","capabilities","assumptions"],
        "additionalProperties": false
      }
    },
    "facts": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "entity_name": { "type": "string" },
          "claim": { "type": "string" },
          "dimension": { "type": "string", "enum": ["what","how","why","when"] },
          "category": { "type": "string", "enum": ["behavior","constraint","pattern","convention","debt","risk","contract"] },
          "confidence": { "type": "string", "enum": ["high","medium","low"] }
        },
        "required": ["entity_name","claim","dimension","category","confidence"],
        "additionalProperties": false
      }
    },
    "relationships": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "from": { "type": "string" },
          "to": { "type": "string" },
          "kind": { "type": "string", "enum": ["depends_on","calls","implements","extends","produces","consumes","replaced_by","tested_by","configured_by","owns"] },
          "description": { "type": "string" },
          "strength": { "type": "string", "enum": ["strong","moderate","weak"] }
        },
        "required": ["from","to","kind","description","strength"],
        "additionalProperties": false
      }
    }
  },
  "required": ["file_summary","entities","facts","relationships"],
  "additionalProperties": false
}`),
}

var SchemaPhase4 = &llm.JSONSchema{
	Name: "phase4_synthesis",
	Schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "architectural_patterns": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "pattern": { "type": "string" },
          "description": { "type": "string" },
          "confidence": { "type": "string", "enum": ["high","medium","low"] }
        },
        "required": ["pattern","description","confidence"],
        "additionalProperties": false
      }
    },
    "data_flows": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "description": { "type": "string" },
          "from_module": { "type": "string" },
          "to_module": { "type": "string" },
          "mechanism": { "type": "string" }
        },
        "required": ["description","from_module","to_module","mechanism"],
        "additionalProperties": false
      }
    },
    "contracts": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "between": { "type": "array", "items": { "type": "string" } },
          "description": { "type": "string" },
          "explicit": { "type": "boolean" }
        },
        "required": ["between","description","explicit"],
        "additionalProperties": false
      }
    },
    "facts": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "entity_name": { "type": "string" },
          "claim": { "type": "string" },
          "dimension": { "type": "string", "enum": ["what","how","why","when"] },
          "category": { "type": "string", "enum": ["behavior","constraint","pattern","convention","debt","risk","contract"] },
          "confidence": { "type": "string", "enum": ["high","medium","low"] }
        },
        "required": ["entity_name","claim","dimension","category","confidence"],
        "additionalProperties": false
      }
    },
    "relationships": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "from": { "type": "string" },
          "to": { "type": "string" },
          "kind": { "type": "string", "enum": ["depends_on","calls","implements","extends","produces","consumes","replaced_by","tested_by","configured_by","owns"] },
          "description": { "type": "string" },
          "strength": { "type": "string", "enum": ["strong","moderate","weak"] }
        },
        "required": ["from","to","kind","description","strength"],
        "additionalProperties": false
      }
    }
  },
  "required": ["architectural_patterns","data_flows","contracts","facts","relationships"],
  "additionalProperties": false
}`),
}

var SchemaPhase5 = &llm.JSONSchema{
	Name: "phase5_summary",
	Schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "summary": { "type": "string" },
    "capabilities": { "type": "array", "items": { "type": "string" } },
    "architecture": { "type": "string" },
    "conventions": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "category": { "type": "string" },
          "description": { "type": "string" },
          "examples": { "type": "array", "items": { "type": "string" } }
        },
        "required": ["category","description","examples"],
        "additionalProperties": false
      }
    },
    "risks_and_debt": { "type": "array", "items": { "type": "string" } },
    "key_integration_points": { "type": "array", "items": { "type": "string" } }
  },
  "required": ["summary","capabilities","architecture","conventions","risks_and_debt","key_integration_points"],
  "additionalProperties": false
}`),
}

var SchemaPhase3 = &llm.JSONSchema{
	Name: "phase3_pr_analysis",
	Schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "facts": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "entity_name": { "type": "string" },
          "claim": { "type": "string" },
          "dimension": { "type": "string", "enum": ["what","how","why","when"] },
          "category": { "type": "string", "enum": ["behavior","constraint","pattern","convention","debt","risk","contract"] },
          "confidence": { "type": "string", "enum": ["high","medium","low"] }
        },
        "required": ["entity_name","claim","dimension","category","confidence"],
        "additionalProperties": false
      }
    },
    "decisions": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "summary": { "type": "string" },
          "description": { "type": "string" },
          "rationale": { "type": "string" },
          "alternatives": {
            "type": "array",
            "items": {
              "type": "object",
              "properties": {
                "description": { "type": "string" },
                "rejected_because": { "type": "string" }
              },
              "required": ["description","rejected_because"],
              "additionalProperties": false
            }
          },
          "tradeoffs": { "type": "array", "items": { "type": "string" } },
          "pr_number": { "type": "integer" },
          "made_at": { "type": "string" }
        },
        "required": ["summary","description","rationale","alternatives","tradeoffs","pr_number","made_at"],
        "additionalProperties": false
      }
    }
  },
  "required": ["facts","decisions"],
  "additionalProperties": false
}`),
}

var SchemaGitLog = &llm.JSONSchema{
	Name: "gitlog_analysis",
	Schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "facts": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "entity_name": { "type": "string" },
          "claim": { "type": "string" },
          "dimension": { "type": "string", "enum": ["what","how","why","when"] },
          "category": { "type": "string", "enum": ["behavior","constraint","pattern","convention","debt","risk","contract"] },
          "confidence": { "type": "string", "enum": ["high","medium","low"] }
        },
        "required": ["entity_name","claim","dimension","category","confidence"],
        "additionalProperties": false
      }
    },
    "decisions": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "summary": { "type": "string" },
          "description": { "type": "string" },
          "rationale": { "type": "string" },
          "made_at": { "type": "string" }
        },
        "required": ["summary","description","rationale","made_at"],
        "additionalProperties": false
      }
    }
  },
  "required": ["facts","decisions"],
  "additionalProperties": false
}`),
}

var SchemaClusterLabel = &llm.JSONSchema{
	Name: "cluster_label",
	Schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "label": { "type": "string" },
    "description": { "type": "string" },
    "domain": { "type": "string" }
  },
  "required": ["label","description","domain"],
  "additionalProperties": false
}`),
}

var SchemaDedup = &llm.JSONSchema{
	Name: "dedup_decision",
	Schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "action": { "type": "string", "enum": ["skip","update","insert","supersede"] },
    "supersedes_id": { "type": ["string","null"] },
    "reason": { "type": "string" }
  },
  "required": ["action","reason"],
  "additionalProperties": false
}`),
}
