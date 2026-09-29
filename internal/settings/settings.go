// Package settings describes typed engine parameters ("adapter settings").
//
// Each adapter publishes a Schema: the settings it understands, their types,
// bounds and the engine flag they map to. Values come from the registry
// (model level, then quant level) and from `--set key=value` on the command
// line; the schema validates them and turns them into engine arguments.
package settings

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Kind is the value type of a setting.
type Kind string

const (
	Int    Kind = "int"
	Float  Kind = "float"
	Bool   Kind = "bool"
	String Kind = "string"
)

// Param is one setting.
type Param struct {
	Name string // registry key, e.g. "context_size"
	Flag string // engine flag, e.g. "--ctx"; Bool flags are passed only when true
	Kind Kind
	// Min/Max bound Int and Float values when Min < Max.
	Min, Max float64
	// Enum restricts String values when non-empty.
	Enum []string
	Help string
}

// Schema is the ordered list of settings an adapter accepts.
type Schema []Param

// Values are validated settings: int64, float64, bool or string.
type Values map[string]any

func (s Schema) lookup(name string) (Param, bool) {
	for _, p := range s {
		if p.Name == name {
			return p, true
		}
	}
	return Param{}, false
}

// Names lists the setting names.
func (s Schema) Names() []string {
	out := make([]string, len(s))
	for i, p := range s {
		out[i] = p.Name
	}
	return out
}

// Validate checks raw values (from YAML or the CLI) against the schema and
// normalizes their types. Strings are parsed for Int, Float and Bool.
func (s Schema) Validate(in map[string]any) (Values, error) {
	out := Values{}
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		p, ok := s.lookup(k)
		if !ok {
			if len(s) == 0 {
				return nil, fmt.Errorf("unknown setting %q (this adapter has no settings)", k)
			}
			return nil, fmt.Errorf("unknown setting %q (known: %s)", k, strings.Join(s.Names(), ", "))
		}
		v, err := p.convert(in[k])
		if err != nil {
			return nil, fmt.Errorf("setting %s: %w", k, err)
		}
		out[k] = v
	}
	return out, nil
}

func (p Param) convert(raw any) (any, error) {
	switch p.Kind {
	case Int:
		var n int64
		switch v := raw.(type) {
		case int:
			n = int64(v)
		case int64:
			n = v
		case uint64:
			if v > math.MaxInt64 {
				return nil, fmt.Errorf("%d is too large", v)
			}
			n = int64(v)
		case float64:
			if v != math.Trunc(v) {
				return nil, fmt.Errorf("want an integer, got %v", v)
			}
			n = int64(v)
		case string:
			x, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("want an integer, got %q", v)
			}
			n = x
		default:
			return nil, fmt.Errorf("want an integer, got %T", raw)
		}
		if p.Min < p.Max && (float64(n) < p.Min || float64(n) > p.Max) {
			return nil, fmt.Errorf("%d is out of range [%g, %g]", n, p.Min, p.Max)
		}
		return n, nil
	case Float:
		var f float64
		switch v := raw.(type) {
		case int:
			f = float64(v)
		case int64:
			f = float64(v)
		case uint64:
			f = float64(v)
		case float64:
			f = v
		case string:
			x, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if err != nil {
				return nil, fmt.Errorf("want a number, got %q", v)
			}
			f = x
		default:
			return nil, fmt.Errorf("want a number, got %T", raw)
		}
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, fmt.Errorf("want a finite number")
		}
		if p.Min < p.Max && (f < p.Min || f > p.Max) {
			return nil, fmt.Errorf("%g is out of range [%g, %g]", f, p.Min, p.Max)
		}
		return f, nil
	case Bool:
		switch v := raw.(type) {
		case bool:
			return v, nil
		case string:
			switch strings.ToLower(strings.TrimSpace(v)) {
			case "true", "on", "yes", "1":
				return true, nil
			case "false", "off", "no", "0":
				return false, nil
			}
			return nil, fmt.Errorf("want true or false, got %q", v)
		}
		return nil, fmt.Errorf("want true or false, got %T", raw)
	case String:
		v, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("want a string, got %T", raw)
		}
		if len(p.Enum) > 0 {
			for _, e := range p.Enum {
				if v == e {
					return v, nil
				}
			}
			return nil, fmt.Errorf("%q is not one of %s", v, strings.Join(p.Enum, ", "))
		}
		if strings.ContainsAny(v, "\x00\n") {
			return nil, fmt.Errorf("invalid characters")
		}
		return v, nil
	}
	return nil, fmt.Errorf("unsupported kind %q", p.Kind)
}

// Args renders validated values as engine arguments, in schema order.
func (s Schema) Args(v Values) []string {
	var out []string
	for _, p := range s {
		x, ok := v[p.Name]
		if !ok || p.Flag == "" {
			continue
		}
		switch t := x.(type) {
		case bool:
			if t {
				out = append(out, p.Flag)
			}
		case int64:
			out = append(out, p.Flag, strconv.FormatInt(t, 10))
		case float64:
			out = append(out, p.Flag, strconv.FormatFloat(t, 'g', -1, 64))
		case string:
			out = append(out, p.Flag, t)
		}
	}
	return out
}

// ParseOverrides parses repeated `key=value` command-line overrides. Values
// stay strings; Schema.Validate converts them.
func ParseOverrides(kv []string) (map[string]any, error) {
	out := map[string]any{}
	for _, s := range kv {
		k, v, ok := strings.Cut(s, "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" {
			return nil, fmt.Errorf("--set %q: want key=value", s)
		}
		out[k] = v
	}
	return out, nil
}

// Merge overlays maps left to right (later wins).
func Merge(ms ...map[string]any) map[string]any {
	out := map[string]any{}
	for _, m := range ms {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

// Format renders values as "a=1 b=true", sorted by key.
func Format(v map[string]any) string {
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%v", k, v[k])
	}
	return strings.Join(parts, " ")
}
