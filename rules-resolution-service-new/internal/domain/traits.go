package domain

import "fmt"

// TraitTypes maps each canonical traitKey to its expected Go type name.
var TraitTypes = map[string]string{
	"slaHours":          "int",
	"requiredDocuments": "[]string",
	"feeAmount":         "int",
	"feeAuthRequired":   "bool",
	"assignedRole":      "string",
	"templateId":        "string",
}

// ValidTraitKeys is the set of known trait keys.
var ValidTraitKeys = map[string]struct{}{
	"slaHours": {}, "requiredDocuments": {}, "feeAmount": {},
	"feeAuthRequired": {}, "assignedRole": {}, "templateId": {},
}

// NormalizeTraitValue coerces raw (decoded from JSONB or incoming JSON) to the
// canonical Go type for traitKey. JSON numbers decode as float64; this function
// converts them to int64 for "int" traits. Returns an error for type mismatches.
func NormalizeTraitValue(traitKey string, raw any) (any, error) {
	expected, ok := TraitTypes[traitKey]
	if !ok {
		return nil, fmt.Errorf("unknown traitKey %q", traitKey)
	}
	switch expected {
	case "int":
		switch v := raw.(type) {
		case float64:
			return int64(v), nil
		case int64:
			return v, nil
		case int:
			return int64(v), nil
		case int32:
			return int64(v), nil
		default:
			return nil, fmt.Errorf("traitKey %q expects int, got %T", traitKey, raw)
		}
	case "bool":
		v, ok := raw.(bool)
		if !ok {
			return nil, fmt.Errorf("traitKey %q expects bool, got %T", traitKey, raw)
		}
		return v, nil
	case "string":
		v, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("traitKey %q expects string, got %T", traitKey, raw)
		}
		return v, nil
	case "[]string":
		switch v := raw.(type) {
		case []string:
			return v, nil
		case []interface{}:
			result := make([]string, len(v))
			for i, item := range v {
				s, ok := item.(string)
				if !ok {
					return nil, fmt.Errorf("traitKey %q: element %d is not a string (got %T)", traitKey, i, item)
				}
				result[i] = s
			}
			return result, nil
		default:
			return nil, fmt.Errorf("traitKey %q expects []string, got %T", traitKey, raw)
		}
	}
	return raw, nil
}
