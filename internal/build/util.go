package build

import (
	"cmp"
	"iter"
	"maps"
	"slices"
)

// sortedOf returns the values of m in key order.
func sortedOf[K cmp.Ordered, V any](m map[K]V) iter.Seq[V] {
	return func(yield func(V) bool) {
		for _, k := range slices.Sorted(maps.Keys(m)) {
			if !yield(m[k]) {
				return
			}
		}
	}
}

// copyBody returns a deep copy of a request body.
func copyBody(body map[string]any) map[string]any {
	out := make(map[string]any, len(body))
	for k, v := range body {
		out[k] = copyValue(v)
	}
	return out
}

// copyValue returns a deep copy of a decoded JSON value.
func copyValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return copyBody(t)
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = copyValue(e)
		}
		return out
	}
	return v
}
