// Package merge merges the changes between two versions of a JSON document
// into a third, as git merges a file: Diff finds what changed from base to
// theirs, as operations at JSON Pointers, and Apply applies them to ours,
// leaving as a conflict a value ours holds otherwise.
//
// Documents are decoded JSON: map[string]any objects, []any arrays, and
// json.Number numbers, as Decode reads them. Diff descends into objects
// alone; an array is one value, as are a string and a number.
package merge

import (
	"bytes"
	"encoding/json"
	"maps"
	"reflect"
	"slices"
	"strings"
)

// The operations of an Op.
const (
	OpAdd     = "add"
	OpReplace = "replace"
	OpRemove  = "remove"
)

// Op is one change of a JSON document, as JSON Patch (RFC 6902) writes one,
// with the value it replaces.
type Op struct {
	// Op is OpAdd, OpReplace or OpRemove.
	Op string `json:"op"`
	// Path is the JSON Pointer (RFC 6901) of the value.
	Path string `json:"path"`
	// Old is the value replaced or removed, and Value the value added or
	// replacing it.
	Old   any `json:"old,omitempty"`
	Value any `json:"value,omitempty"`
}

// Conflict is an Op that Apply did not apply, because ours holds the value
// otherwise than its base did, and what ours holds.
type Conflict struct {
	Op
	// Ours is the value ours holds at Op's path, nil when it holds none.
	Ours any `json:"ours,omitempty"`
	// Why says why the Op was not applied.
	Why string `json:"why"`
}

// Decode reads JSON document data, with its numbers as json.Number, as Diff
// and Apply take documents.
func Decode(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// Diff returns the operations that turn base into theirs, in path order:
// an add for each key theirs adds to an object, a remove for each it drops,
// and a replace for each other value it changes. It leaves out the paths
// skip reports, given as their keys from the root.
func Diff(base, theirs any, skip func(path []string) bool) []Op {
	var ops []Op
	diff(base, theirs, nil, skip, &ops)
	return ops
}

func diff(base, theirs any, path []string, skip func([]string) bool, ops *[]Op) {
	if skip != nil && skip(path) {
		return
	}
	b, bok := base.(map[string]any)
	t, tok := theirs.(map[string]any)
	if !bok || !tok {
		if !reflect.DeepEqual(base, theirs) {
			*ops = append(*ops, Op{Op: OpReplace, Path: Pointer(path), Old: base, Value: theirs})
		}
		return
	}
	keys := slices.Sorted(maps.Keys(b))
	for k := range t {
		if _, ok := b[k]; !ok {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	for _, k := range keys {
		at := append(slices.Clone(path), k)
		bv, inBase := b[k]
		tv, inTheirs := t[k]
		switch {
		case skip != nil && skip(at):
		case !inBase:
			*ops = append(*ops, Op{Op: OpAdd, Path: Pointer(at), Value: tv})
		case !inTheirs:
			*ops = append(*ops, Op{Op: OpRemove, Path: Pointer(at), Old: bv})
		default:
			diff(bv, tv, at, skip, ops)
		}
	}
}

// Apply applies ops to ours, in place, and returns it and the ops it did
// not apply:
//
//   - An add sets its value, unless ours holds another there already, or
//     lacks the object to add it to.
//   - A replace sets its value where ours holds its Old, or the value
//     already; not where ours holds another, which it changed too, or none.
//   - A remove removes the value ours holds, whatever it is, as an
//     upstream that drops something drops what ours added to it too.
//
// ours must be an object.
func Apply(ours any, ops []Op) (any, []Conflict) {
	var conflicts []Conflict
	for _, op := range ops {
		path := ParsePointer(op.Path)
		if len(path) == 0 {
			conflicts = append(conflicts, Conflict{Op: op, Why: "the root is no value to change"})
			continue
		}
		parent, ok := lookup(ours, path[:len(path)-1]).(map[string]any)
		key := path[len(path)-1]
		cur, has := parent[key]
		switch op.Op {
		case OpAdd:
			switch {
			case !ok:
				conflicts = append(conflicts, Conflict{Op: op, Why: "ours lacks the object to add it to"})
			case has && !reflect.DeepEqual(cur, op.Value):
				conflicts = append(conflicts, Conflict{Op: op, Ours: cur, Why: "ours holds another value there"})
			default:
				parent[key] = op.Value
			}
		case OpReplace:
			switch {
			case !has:
				conflicts = append(conflicts, Conflict{Op: op, Why: "ours holds no value there"})
			case reflect.DeepEqual(cur, op.Value):
			case reflect.DeepEqual(cur, op.Old):
				parent[key] = op.Value
			default:
				conflicts = append(conflicts, Conflict{Op: op, Ours: cur, Why: "ours changed it too"})
			}
		case OpRemove:
			delete(parent, key)
		default:
			conflicts = append(conflicts, Conflict{Op: op, Why: "no such operation"})
		}
	}
	return ours, conflicts
}

// lookup returns the value of doc at path, or nil when doc has none.
func lookup(doc any, path []string) any {
	for _, k := range path {
		obj, ok := doc.(map[string]any)
		if !ok {
			return nil
		}
		doc = obj[k]
	}
	return doc
}

var pointerEscaper = strings.NewReplacer("~", "~0", "/", "~1")

// Pointer returns the JSON Pointer of path, the keys from the root.
func Pointer(path []string) string {
	var b strings.Builder
	for _, k := range path {
		b.WriteByte('/')
		b.WriteString(pointerEscaper.Replace(k))
	}
	return b.String()
}

var pointerUnescaper = strings.NewReplacer("~1", "/", "~0", "~")

// ParsePointer returns the keys from the root of JSON Pointer p.
func ParsePointer(p string) []string {
	if p == "" {
		return nil
	}
	keys := strings.Split(strings.TrimPrefix(p, "/"), "/")
	for i, k := range keys {
		keys[i] = pointerUnescaper.Replace(k)
	}
	return keys
}
