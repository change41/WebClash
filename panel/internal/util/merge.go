package util

// IsPlainObject reports whether v is a JSON object (map), not null/array.
func IsPlainObject(v any) bool {
	_, ok := v.(map[string]any)
	return ok
}

// DeepMerge merges patch into base (port of util.ts deepMerge). Objects merge
// recursively; arrays and scalars replace wholesale. JSON null is a value and
// participates in the merge (only absent keys are skipped).
func DeepMerge(base, patch any) any {
	baseMap, baseOk := base.(map[string]any)
	patchMap, patchOk := patch.(map[string]any)
	if !baseOk || !patchOk {
		return patch
	}
	out := make(map[string]any, len(baseMap)+len(patchMap))
	for k, v := range baseMap {
		out[k] = v
	}
	for k, v := range patchMap {
		cur := baseMap[k]
		if IsPlainObject(v) && IsPlainObject(cur) {
			out[k] = DeepMerge(cur, v)
		} else {
			out[k] = v
		}
	}
	return out
}
