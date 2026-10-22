// Package jsonschema writes the JSON Schema (draft 2020-12) of the JSON
// documents encoding/json encodes a Go type as, in the document's own terms:
// its definitions are named in snake_case, as the documents' keys are, and
// described by a file keyed by those names, not by Go doc comments. It
// validates documents against the schemas it writes too.
package jsonschema

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Draft is the JSON Schema dialect Generate writes.
const Draft = "https://json-schema.org/draft/2020-12/schema"

// Descriptions describe a schema's definitions, by their snake_case names,
// as a file of them holds them in JSON.
type Descriptions map[string]Description

// Description describes a definition and its properties, by the
// properties' JSON names.
type Description struct {
	Description string            `json:"description"`
	Examples    []string          `json:"examples,omitempty"`
	Properties  map[string]string `json:"properties,omitempty"`
}

// ReadConsts returns the string constants of the named types of the package
// in dir, by the type's Go name, in source order, leaving out its tests.
// Generate gives a definition them as examples, unless its description has
// examples of its own.
func ReadConsts(dir string) (map[string][]string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	return ReadConstsOf(slices.DeleteFunc(files, func(name string) bool { return strings.HasSuffix(name, "_test.go") })...)
}

// ReadConstsOf returns the string constants of the named types of the Go
// files names, as ReadConsts does.
func ReadConstsOf(names ...string) (map[string][]string, error) {
	consts := map[string][]string{}
	fset := token.NewFileSet()
	for _, name := range names {
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("jsonschema: %w", err)
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				s := spec.(*ast.ValueSpec)
				id, ok := s.Type.(*ast.Ident)
				if !ok {
					continue
				}
				for _, v := range s.Values {
					if lit, ok := v.(*ast.BasicLit); ok && lit.Kind == token.STRING {
						if u, err := strconv.Unquote(lit.Value); err == nil {
							consts[id.Name] = append(consts[id.Name], u)
						}
					}
				}
			}
		}
	}
	return consts, nil
}

// Snake returns Go name name in snake_case, as "PriceTier" is "price_tier"
// and "ModelID" is "model_id".
func Snake(name string) string {
	rs := []rune(name)
	var b strings.Builder
	for i, r := range rs {
		if unicode.IsUpper(r) {
			if i > 0 && (!unicode.IsUpper(rs[i-1]) || i+1 < len(rs) && unicode.IsLower(rs[i+1])) {
				b.WriteByte('_')
			}
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Options configure Generate.
type Options struct {
	// ID is the schema's $id: the URL it is published at.
	ID string
	// Title is the schema's title.
	Title string
	// Descriptions describe every definition and every property. Generate
	// fails on one they leave out, and on one they describe that the
	// schema has not.
	Descriptions Descriptions
	// Consts are the string constants of the root type's package, by Go
	// type name, as ReadConsts returns them.
	Consts map[string][]string
	// Properties adds keywords to properties, by "definition.property", as
	// {"const": 7} pins a version.
	Properties map[string]map[string]any
}

// Generate returns the JSON Schema of the documents encoding/json encodes a
// value of root as, indented. Every named type of root's package is a
// definition, named in snake_case.
func Generate(root reflect.Type, opts Options) ([]byte, error) {
	g := &generator{opts: opts, pkg: root.PkgPath(), defs: map[string]any{}, described: map[string]bool{}}
	doc := g.schema(root)
	doc["$schema"] = Draft
	if opts.ID != "" {
		doc["$id"] = opts.ID
	}
	if opts.Title != "" {
		doc["title"] = opts.Title
	}
	doc["$defs"] = g.defs
	for _, def := range slices.Sorted(maps.Keys(opts.Descriptions)) {
		if !g.described[def] {
			g.fail("%s is described, but no definition", def)
		}
		for _, p := range slices.Sorted(maps.Keys(opts.Descriptions[def].Properties)) {
			if !g.described[def+"."+p] {
				g.fail("%s.%s is described, but no property", def, p)
			}
		}
	}
	if g.err != nil {
		return nil, g.err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("jsonschema: %w", err)
	}
	return buf.Bytes(), nil
}

// generator writes the schemas of a package's types into its definitions.
type generator struct {
	opts Options
	pkg  string
	defs map[string]any
	// described holds the definitions, and their properties as
	// "definition.property", that the schema has.
	described map[string]bool
	err       error
}

func (g *generator) fail(format string, args ...any) {
	g.err = errors.Join(g.err, fmt.Errorf("jsonschema: "+format, args...))
}

var timeType = reflect.TypeFor[time.Time]()

// schema returns the schema of t: a reference to its definition when t is a
// named type of the package.
func (g *generator) schema(t reflect.Type) map[string]any {
	switch {
	case t == timeType:
		return map[string]any{"type": "string", "format": "date-time"}
	case t.Kind() == reflect.Pointer:
		return g.schema(t.Elem())
	case t.Kind() == reflect.Interface:
		return map[string]any{}
	case t.Name() == "" || t.PkgPath() != g.pkg:
		return g.define(t, "")
	}
	def := Snake(t.Name())
	if _, ok := g.defs[def]; !ok {
		g.defs[def] = map[string]any{} // stops a recursive type
		g.defs[def] = g.define(t, def)
	}
	return map[string]any{"$ref": "#/$defs/" + def}
}

// define returns the schema of t itself, as definition def, or "" when t is
// no definition.
func (g *generator) define(t reflect.Type, def string) map[string]any {
	s := map[string]any{}
	switch t.Kind() {
	case reflect.Struct:
		props := map[string]any{}
		var required []string
		g.fields(t, def, props, &required)
		s["type"], s["properties"] = "object", props
		if len(required) > 0 {
			s["required"] = required
		}
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			g.fail("map key of %s is not a string", t)
		}
		s["type"], s["additionalProperties"] = "object", g.schema(t.Elem())
		if t.Key().Name() != "" && t.Key().PkgPath() == g.pkg {
			s["propertyNames"] = g.schema(t.Key())
		}
	case reflect.Slice, reflect.Array:
		s["type"], s["items"] = "array", g.schema(t.Elem())
	case reflect.String:
		s["type"] = "string"
	case reflect.Bool:
		s["type"] = "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		s["type"] = "integer"
	case reflect.Float32, reflect.Float64:
		s["type"] = "number"
	default:
		g.fail("unsupported type %s", t)
	}
	if def == "" {
		return s
	}
	g.described[def] = true
	d := g.opts.Descriptions[def]
	if d.Description == "" {
		g.fail("definition %s has no description", def)
	}
	s["description"] = d.Description
	ex := d.Examples
	if len(ex) == 0 {
		ex = g.opts.Consts[t.Name()]
	}
	if len(ex) > 0 {
		s["examples"] = ex
	}
	return s
}

// fields adds the properties of struct t, of definition def, to props, and
// the names of those encoding/json always writes to required. It inlines
// the fields of an embedded struct, as encoding/json does.
func (g *generator) fields(t reflect.Type, def string, props map[string]any, required *[]string) {
	for i := range t.NumField() {
		f := t.Field(i)
		name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" && opts == "" {
			continue
		}
		if f.Anonymous && name == "" {
			if ft := derefType(f.Type); ft.Kind() == reflect.Struct {
				g.fields(ft, def, props, required)
				continue
			}
		}
		if !f.IsExported() {
			continue
		}
		name = cmp.Or(name, f.Name)
		optional := slices.Contains(strings.Split(opts, ","), "omitempty") || slices.Contains(strings.Split(opts, ","), "omitzero")
		p := g.schema(f.Type)
		switch f.Type.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Map:
			if !optional {
				p = map[string]any{"anyOf": []any{p, map[string]any{"type": "null"}}}
			}
		}
		if def != "" {
			g.described[def+"."+name] = true
			desc := g.opts.Descriptions[def].Properties[name]
			if desc == "" {
				g.fail("property %s.%s has no description", def, name)
			}
			p["description"] = desc
		}
		maps.Copy(p, g.opts.Properties[def+"."+name])
		props[name] = p
		if !optional {
			*required = append(*required, name)
		}
	}
}

func derefType(t reflect.Type) reflect.Type {
	if t.Kind() == reflect.Pointer {
		return t.Elem()
	}
	return t
}

// maxErrors bounds the errors Validate reports.
const maxErrors = 20

// Validate reports where doc, a JSON document, breaks schema, a JSON Schema
// as Generate writes it, each error at its JSON Pointer. It checks the
// keywords Generate writes that constrain: $ref to $defs, type, const,
// anyOf, properties, required, additionalProperties, propertyNames and
// items; and pattern, which Options.Properties may add.
func Validate(schema, doc []byte) error {
	var s map[string]any
	if err := decode(schema, &s); err != nil {
		return fmt.Errorf("jsonschema: schema: %w", err)
	}
	var d any
	if err := decode(doc, &d); err != nil {
		return fmt.Errorf("jsonschema: document: %w", err)
	}
	defs, _ := s["$defs"].(map[string]any)
	v := &validator{defs: defs}
	v.check(s, d, "")
	if len(v.errs) > maxErrors {
		v.errs = append(v.errs[:maxErrors], fmt.Errorf("and %d more", len(v.errs)-maxErrors))
	}
	return errors.Join(v.errs...)
}

func decode(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return dec.Decode(v)
}

// validator collects the errors of a document against a schema.
type validator struct {
	defs map[string]any
	errs []error
}

func (v *validator) fail(path, format string, args ...any) {
	v.errs = append(v.errs, fmt.Errorf("%s: %s", cmp.Or(path, "/"), fmt.Sprintf(format, args...)))
}

// check checks d, at path, against schema s.
func (v *validator) check(s map[string]any, d any, path string) {
	if ref, ok := s["$ref"].(string); ok {
		name, _ := strings.CutPrefix(ref, "#/$defs/")
		def, ok := v.defs[name].(map[string]any)
		if !ok {
			v.fail(path, "no definition %s", ref)
			return
		}
		v.check(def, d, path)
	}
	if c, ok := s["const"]; ok && !reflect.DeepEqual(c, d) {
		v.fail(path, "is %v, want %v", d, c)
	}
	if alts, ok := s["anyOf"].([]any); ok {
		// A value that is not null breaks a nullable property as it breaks
		// the property's other schema, so that is what to report.
		var tried [][]error
		matched := false
		for _, a := range alts {
			as, _ := a.(map[string]any)
			sub := &validator{defs: v.defs}
			sub.check(as, d, path)
			if matched = len(sub.errs) == 0; matched {
				break
			}
			if d == nil || as["type"] != "null" {
				tried = append(tried, sub.errs)
			}
		}
		switch {
		case matched:
		case len(tried) == 1:
			v.errs = append(v.errs, tried[0]...)
		default:
			v.fail(path, "matches none of anyOf")
		}
	}
	if t, ok := s["type"].(string); ok && !isType(d, t) {
		v.fail(path, "is %s, want %s", typeOf(d), t)
		return
	}
	if p, ok := s["pattern"].(string); ok {
		if str, ok := d.(string); ok {
			if re, err := regexp.Compile(p); err != nil {
				v.fail(path, "pattern %q: %v", p, err)
			} else if !re.MatchString(str) {
				v.fail(path, "is %q, want it to match %s", str, p)
			}
		}
	}
	switch d := d.(type) {
	case map[string]any:
		props, _ := s["properties"].(map[string]any)
		required, _ := s["required"].([]any)
		for _, r := range required {
			if _, ok := d[r.(string)]; !ok {
				v.fail(path, "has no %q", r)
			}
		}
		extra, _ := s["additionalProperties"].(map[string]any)
		names, _ := s["propertyNames"].(map[string]any)
		for _, k := range slices.Sorted(maps.Keys(d)) {
			at := path + "/" + strings.NewReplacer("~", "~0", "/", "~1").Replace(k)
			if names != nil {
				v.check(names, k, at)
			}
			if p, ok := props[k].(map[string]any); ok {
				v.check(p, d[k], at)
			} else if extra != nil {
				v.check(extra, d[k], at)
			}
		}
	case []any:
		if items, ok := s["items"].(map[string]any); ok {
			for i, e := range d {
				v.check(items, e, path+"/"+strconv.Itoa(i))
			}
		}
	}
}

// typeOf returns the JSON Schema type of d, as decode decodes it.
func typeOf(d any) string {
	switch d := d.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case json.Number:
		if _, err := d.Int64(); err == nil {
			return "integer"
		}
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return fmt.Sprintf("%T", d)
}

// isType reports whether d is of JSON Schema type t, an integer being a
// number too.
func isType(d any, t string) bool {
	got := typeOf(d)
	return got == t || t == "number" && got == "integer"
}
