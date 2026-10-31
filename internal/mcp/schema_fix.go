package mcp

// ModelSchema makes a tool's input schema one the model APIs accept
// (AI-8): the root must be "type": "object" and may not be anyOf /
// oneOf / allOf. Root branches are merged into one object (their
// properties together; only the root's own "required" is kept, since
// each branch needs different fields). in is not changed.
func ModelSchema(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	_, any1 := in["anyOf"]
	_, one := in["oneOf"]
	_, all := in["allOf"]
	if in["type"] == "object" && !any1 && !one && !all {
		return in
	}
	out := map[string]any{}
	for k, v := range in {
		switch k {
		case "anyOf", "oneOf", "allOf", "type", "properties":
		default:
			out[k] = v
		}
	}
	props := map[string]any{}
	if p, ok := in["properties"].(map[string]any); ok && in["type"] == "object" {
		for k, v := range p {
			props[k] = v
		}
	}
	for _, k := range []string{"allOf", "anyOf", "oneOf"} {
		branches, _ := in[k].([]any)
		for _, b := range branches {
			bm, _ := b.(map[string]any)
			p, _ := bm["properties"].(map[string]any)
			for name, v := range p {
				if _, ok := props[name]; !ok {
					props[name] = v
				}
			}
		}
	}
	out["type"] = "object"
	out["properties"] = props
	return out
}
