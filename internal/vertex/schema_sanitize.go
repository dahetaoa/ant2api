package vertex

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// SanitizeFunctionParametersSchema converts a JSON-Schema-ish map (often produced by Claude/OpenAI tool schemas)
// into the subset of OpenAPI Schema that Vertex tool/functionDeclarations.parameters accepts.
//
// Vertex rejects unknown fields (e.g. "$schema", "exclusiveMinimum"), so this function:
// - Deep-copies the schema (no in-place mutation of the caller input)
// - Removes/renames unsupported keys (e.g. $ref -> ref, $defs -> defs)
// - Normalizes type/enums and drops unsupported JSON Schema keywords
func SanitizeFunctionParametersSchema(schema map[string]any) map[string]any {
	if schema == nil {
		return nil
	}
	outAny := deepCopyAny(schema)
	out, _ := outAny.(map[string]any)
	if out == nil {
		return nil
	}
	sanitizeVertexSchemaInPlace(out)
	return out
}

func deepCopyAny(v any) any {
	switch t := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(t))
		for k, v2 := range t {
			m[k] = deepCopyAny(v2)
		}
		return m
	case []any:
		s := make([]any, len(t))
		for i, v2 := range t {
			s[i] = deepCopyAny(v2)
		}
		return s
	default:
		return v
	}
}

func sanitizeVertexSchemaInPlace(schema map[string]any) {
	if schema == nil {
		return
	}

	// Remove unsupported/metadata keys early.
	delete(schema, "$schema")
	delete(schema, "$id")
	delete(schema, "$anchor")

	// Vertex Schema uses "ref"/"defs" (no $ prefix).
	if v, ok := schema["$ref"]; ok {
		if _, has := schema["ref"]; !has {
			schema["ref"] = v
		}
		delete(schema, "$ref")
	}
	if v, ok := schema["$defs"]; ok {
		if _, has := schema["defs"]; !has {
			schema["defs"] = v
		}
		delete(schema, "$defs")
	}
	if v, ok := schema["definitions"]; ok {
		if _, has := schema["defs"]; !has {
			schema["defs"] = v
		}
		delete(schema, "definitions")
	}

	// Convert oneOf -> anyOf if needed; Vertex supports anyOf.
	if v, ok := schema["oneOf"]; ok {
		if _, has := schema["anyOf"]; !has {
			schema["anyOf"] = v
		} else if dst, okDst := schema["anyOf"].([]any); okDst {
			if src, okSrc := v.([]any); okSrc {
				schema["anyOf"] = append(dst, src...)
			}
		}
		delete(schema, "oneOf")
	}
	// allOf is not supported; best-effort fallback to the first entry.
	if v, ok := schema["allOf"]; ok {
		if arr, okArr := v.([]any); okArr && len(arr) > 0 {
			if first, okFirst := arr[0].(map[string]any); okFirst {
				for k, vv := range first {
					if _, exists := schema[k]; !exists {
						schema[k] = vv
					}
				}
			}
		}
		delete(schema, "allOf")
	}

	// exclusiveMinimum/exclusiveMaximum are not supported by Vertex Schema.
	convertExclusiveBounds(schema)

	// Normalize "type" (Vertex expects enum names like "OBJECT", "STRING"...).
	normalizeTypeField(schema)

	// Normalize "enum" to []string (Vertex Schema uses string enums).
	if enum, ok := schema["enum"]; ok {
		if v := normalizeEnum(enum); v != nil {
			schema["enum"] = v
		} else {
			delete(schema, "enum")
		}
	}

	// Normalize "required" to []string.
	if req, ok := schema["required"]; ok {
		if v := normalizeStringArray(req); v != nil {
			schema["required"] = v
		} else {
			delete(schema, "required")
		}
	}

	// Normalize numeric bounds to numbers.
	if v, ok := schema["minimum"]; ok {
		if f, okF := toFloat64(v); okF {
			schema["minimum"] = f
		} else {
			delete(schema, "minimum")
		}
	}
	if v, ok := schema["maximum"]; ok {
		if f, okF := toFloat64(v); okF {
			schema["maximum"] = f
		} else {
			delete(schema, "maximum")
		}
	}

	// Remove JSON Schema keywords not supported by Vertex Schema.
	for _, k := range []string{
		// Draft keywords / unsupported combinators.
		"not",
		"if",
		"then",
		"else",
		"dependentSchemas",
		"dependentRequired",
		"dependencies",
		"patternProperties",
		"propertyNames",
		"unevaluatedProperties",
		"unevaluatedItems",
		"prefixItems",
		"contains",
		"minContains",
		"maxContains",
		"multipleOf",
		"pattern",
		"format",
		"minItems",
		"maxItems",
		"uniqueItems",
		"minLength",
		"maxLength",
		"minProperties",
		"maxProperties",
		"additionalProperties",
		// Media annotations.
		"contentMediaType",
		"contentEncoding",
		// Misc JSON Schema validation keywords that are commonly sent but not accepted.
		"const",
		"examples",
		"readOnly",
		"writeOnly",
		"deprecated",
	} {
		delete(schema, k)
	}

	// Recurse into defs (if present).
	if defs, ok := schema["defs"].(map[string]any); ok {
		for k, v := range defs {
			m, okM := v.(map[string]any)
			if !okM {
				delete(defs, k)
				continue
			}
			sanitizeVertexSchemaInPlace(m)
		}
	} else if _, has := schema["defs"]; has {
		// defs must be an object
		delete(schema, "defs")
	}

	// Recurse into properties.
	if props, ok := schema["properties"].(map[string]any); ok {
		for k, v := range props {
			m, okM := v.(map[string]any)
			if !okM {
				delete(props, k)
				continue
			}
			sanitizeVertexSchemaInPlace(m)
		}
	} else if _, has := schema["properties"]; has {
		// properties must be an object
		delete(schema, "properties")
	}

	// Recurse into items.
	switch items := schema["items"].(type) {
	case map[string]any:
		sanitizeVertexSchemaInPlace(items)
	case []any:
		// JSON Schema allows array form; Vertex expects a single Schema.
		for _, it := range items {
			if m, okM := it.(map[string]any); okM {
				sanitizeVertexSchemaInPlace(m)
				schema["items"] = m
				break
			}
		}
		if _, ok := schema["items"].(map[string]any); !ok {
			delete(schema, "items")
		}
	default:
		if _, has := schema["items"]; has {
			delete(schema, "items")
		}
	}

	// Recurse into anyOf.
	if arr, ok := schema["anyOf"].([]any); ok {
		dst := make([]any, 0, len(arr))
		for _, it := range arr {
			m, okM := it.(map[string]any)
			if !okM {
				continue
			}
			sanitizeVertexSchemaInPlace(m)
			dst = append(dst, m)
		}
		if len(dst) == 0 {
			delete(schema, "anyOf")
		} else {
			schema["anyOf"] = dst
		}
	} else if _, has := schema["anyOf"]; has {
		delete(schema, "anyOf")
	}

	enforceVertexSchemaAllowlist(schema)
}

func normalizeTypeField(schema map[string]any) {
	raw, ok := schema["type"]
	if !ok {
		return
	}
	switch t := raw.(type) {
	case string:
		if norm, ok := normalizeVertexType(t); ok {
			schema["type"] = norm
		}
	case []any:
		// JSON Schema union types like ["string","null"].
		var hasNull bool
		var firstNonNull string
		for _, it := range t {
			s, okS := it.(string)
			if !okS {
				continue
			}
			if strings.EqualFold(s, "null") {
				hasNull = true
				continue
			}
			if firstNonNull == "" {
				firstNonNull = s
			}
		}
		if hasNull {
			if _, exists := schema["nullable"]; !exists {
				schema["nullable"] = true
			}
		}
		if firstNonNull != "" {
			if norm, ok := normalizeVertexType(firstNonNull); ok {
				schema["type"] = norm
			} else {
				schema["type"] = strings.ToUpper(firstNonNull)
			}
		} else {
			delete(schema, "type")
		}
	default:
		// If it's an unexpected type, drop it instead of letting Vertex reject the schema.
		delete(schema, "type")
	}
}

func normalizeVertexType(t string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "object":
		return "OBJECT", true
	case "array":
		return "ARRAY", true
	case "string":
		return "STRING", true
	case "integer", "int":
		return "INTEGER", true
	case "number":
		return "NUMBER", true
	case "boolean", "bool":
		return "BOOLEAN", true
	case "null":
		return "NULL", true
	default:
		// Already a Vertex enum or something else.
		up := strings.ToUpper(strings.TrimSpace(t))
		switch up {
		case "TYPE_UNSPECIFIED", "STRING", "NUMBER", "INTEGER", "BOOLEAN", "ARRAY", "OBJECT", "NULL":
			return up, true
		default:
			return "", false
		}
	}
}

func normalizeEnum(v any) any {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, it := range t {
			switch vv := it.(type) {
			case string:
				out = append(out, vv)
			case float64:
				out = append(out, trimTrailingDotZero(fmt.Sprintf("%v", vv)))
			case bool:
				out = append(out, strconv.FormatBool(vv))
			default:
				out = append(out, fmt.Sprintf("%v", vv))
			}
		}
		return out
	default:
		// Vertex expects an array; drop invalid forms.
		return nil
	}
}

func normalizeStringArray(v any) any {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, it := range t {
			s, ok := it.(string)
			if !ok {
				continue
			}
			if strings.TrimSpace(s) == "" {
				continue
			}
			out = append(out, s)
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return nil
	}
}

func trimTrailingDotZero(s string) string {
	if strings.HasSuffix(s, ".0") {
		return strings.TrimSuffix(s, ".0")
	}
	return s
}

func enforceVertexSchemaAllowlist(schema map[string]any) {
	// Vertex tool schema parsing is strict: unknown fields cause 400.
	// Keep a conservative allowlist for maximum compatibility.
	allowed := map[string]struct{}{
		"type":        {},
		"properties":  {},
		"required":    {},
		"description": {},
		"enum":        {},
		"items":       {},
		"nullable":    {},
		"minimum":     {},
		"maximum":     {},
		"anyOf":       {},
		"ref":         {},
		"defs":        {},
	}
	for k := range schema {
		if strings.HasPrefix(k, "$") {
			delete(schema, k)
			continue
		}
		if _, ok := allowed[k]; !ok {
			delete(schema, k)
		}
	}

	// Final type checks for kept fields to avoid map values that Vertex cannot parse.
	if v, ok := schema["ref"]; ok {
		if _, okS := v.(string); !okS {
			delete(schema, "ref")
		}
	}
	if v, ok := schema["type"]; ok {
		if _, okS := v.(string); !okS {
			delete(schema, "type")
		}
	}
	if v, ok := schema["description"]; ok {
		if _, okS := v.(string); !okS {
			delete(schema, "description")
		}
	}
	if v, ok := schema["nullable"]; ok {
		if _, okB := v.(bool); !okB {
			delete(schema, "nullable")
		}
	}
}

func convertExclusiveBounds(schema map[string]any) {
	// numeric exclusiveMinimum in JSON Schema (draft 2019-09/2020-12)
	if exMin, ok := schema["exclusiveMinimum"]; ok {
		if _, hasMin := schema["minimum"]; !hasMin {
			if v, okV := toFloat64(exMin); okV {
				schema["minimum"] = adjustExclusive(v, schema, true)
			}
		} else if exB, okB := exMin.(bool); okB && exB {
			// draft-04 style: exclusiveMinimum=true with "minimum" set.
			if v, okV := toFloat64(schema["minimum"]); okV {
				schema["minimum"] = adjustExclusive(v, schema, true)
			}
		}
		delete(schema, "exclusiveMinimum")
	}

	if exMax, ok := schema["exclusiveMaximum"]; ok {
		if _, hasMax := schema["maximum"]; !hasMax {
			if v, okV := toFloat64(exMax); okV {
				schema["maximum"] = adjustExclusive(v, schema, false)
			}
		} else if exB, okB := exMax.(bool); okB && exB {
			if v, okV := toFloat64(schema["maximum"]); okV {
				schema["maximum"] = adjustExclusive(v, schema, false)
			}
		}
		delete(schema, "exclusiveMaximum")
	}
}

func toFloat64(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		if err != nil {
			return 0, false
		}
		return f, true
	case json.Number:
		f, err := t.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

func adjustExclusive(bound float64, schema map[string]any, isMin bool) float64 {
	// If the schema is explicitly INTEGER, we can preserve exclusivity by +/- 1 for whole-number bounds.
	t, _ := schema["type"].(string)
	if strings.EqualFold(t, "INTEGER") {
		if isWholeNumber(bound) {
			if isMin {
				return bound + 1
			}
			return bound - 1
		}
	}
	// For NUMBER (or unknown), fall back to inclusive bound (best effort).
	return bound
}

func isWholeNumber(f float64) bool {
	return f == float64(int64(f))
}
