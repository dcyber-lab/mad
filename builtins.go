package jqgo

import (
	_ "embed"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

type valueFunc func(e *evaluator, v any, args []any) (any, error)

type genFunc func(e *evaluator, n *callNode, env *envT, v any, p *pathT, out emitFunc) error

// globalFunc is a function resolved at compile time: a jq-defined builtin
// (def), a native value function (fn) or a native generator (gen).
type globalFunc struct {
	name  string
	arity int
	def   *funcDef
	fn    valueFunc
	gen   genFunc
}

//go:embed builtin.jq
var builtinSrc string

var (
	builtinsOnce sync.Once
	builtinsErr  error
	globals      map[string]*globalFunc // "name/arity"
)

func funcKey(name string, arity int) string {
	return fmt.Sprintf("%s/%d", name, arity)
}

func loadBuiltins() error {
	builtinsOnce.Do(func() {
		globals = map[string]*globalFunc{}
		for k, g := range natives {
			globals[k] = g
		}
		defs, err := parseDefs(builtinSrc)
		if err != nil {
			builtinsErr = fmt.Errorf("jqgo: builtin library: %w", err)
			return
		}
		for _, d := range defs {
			globals[funcKey(d.name, len(d.params))] = &globalFunc{name: d.name, arity: len(d.params), def: d}
		}
		c := &checker{vars: map[string]bool{"ENV": true}, lookup: func(name string, arity int) *globalFunc { return globals[funcKey(name, arity)] }}
		for _, d := range defs {
			if err := c.checkDefIn(d, nil); err != nil {
				builtinsErr = fmt.Errorf("jqgo: builtin library: %s: %w", d.name, err)
				return
			}
		}
	})
	return builtinsErr
}

var natives = map[string]*globalFunc{}

func defFn(name string, arity int, fn valueFunc) {
	natives[funcKey(name, arity)] = &globalFunc{name: name, arity: arity, fn: fn}
}

func defGen(name string, arity int, fn genFunc) {
	natives[funcKey(name, arity)] = &globalFunc{name: name, arity: arity, gen: fn}
}

func init() {
	defGen("empty", 0, func(e *evaluator, n *callNode, env *envT, v any, p *pathT, out emitFunc) error {
		return nil
	})
	defFn("error", 0, func(e *evaluator, v any, _ []any) (any, error) {
		return nil, &ValueError{Value: v}
	})
	defFn("not", 0, func(e *evaluator, v any, _ []any) (any, error) { return !truthy(v), nil })
	defGen("select", 1, func(e *evaluator, n *callNode, env *envT, v any, p *pathT, out emitFunc) error {
		return e.evalArg(n, 0, env, v, func(c any) error {
			if truthy(c) {
				return out(v, p)
			}
			return nil
		})
	})
	defGen("path", 1, func(e *evaluator, n *callNode, env *envT, v any, p *pathT, out emitFunc) error {
		return e.run(n.args[0], env, v, rootPath, func(_ any, xp *pathT) error {
			arr, err := xp.toArray()
			if err != nil {
				return err
			}
			return emitValue(out, arr, p)
		})
	})
	defGen("getpath", 1, func(e *evaluator, n *callNode, env *envT, v any, p *pathT, out emitFunc) error {
		return e.evalArg(n, 0, env, v, func(pa any) error {
			path, ok := pa.([]any)
			if !ok {
				return fmt.Errorf("Path must be specified as an array")
			}
			r, err := getPath(v, path)
			if err != nil {
				return err
			}
			if p == nil {
				return out(r, nil)
			}
			if p.invalid {
				return fmt.Errorf("Invalid path expression with result %s", dumpTrunc(p.value))
			}
			np := p
			for _, k := range path {
				np = &pathT{parent: np, key: k}
			}
			return out(r, np)
		})
	})
	defFn("setpath", 2, func(e *evaluator, v any, args []any) (any, error) {
		path, ok := args[0].([]any)
		if !ok {
			return nil, fmt.Errorf("Path must be specified as an array")
		}
		return setPath(v, path, args[1], nil)
	})
	defFn("delpaths", 1, func(e *evaluator, v any, args []any) (any, error) {
		ps, ok := args[0].([]any)
		if !ok {
			return nil, fmt.Errorf("Paths must be specified as an array")
		}
		paths := make([][]any, len(ps))
		for i, x := range ps {
			path, ok := x.([]any)
			if !ok {
				return nil, fmt.Errorf("Path must be specified as an array")
			}
			paths[i] = path
		}
		return delPaths(v, paths)
	})
	defGen("recurse", 0, func(e *evaluator, n *callNode, env *envT, v any, p *pathT, out emitFunc) error {
		return e.recurseAll(v, p, out)
	})
	defGen("recurse", 1, func(e *evaluator, n *callNode, env *envT, v any, p *pathT, out emitFunc) error {
		var rec func(x any, xp *pathT) error
		rec = func(x any, xp *pathT) error {
			if err := out(x, xp); err != nil {
				return err
			}
			return e.run(n.args[0], env, x, xp, rec)
		}
		return rec(v, p)
	})
	defGen("limit", 2, func(e *evaluator, n *callNode, env *envT, v any, p *pathT, out emitFunc) error {
		return e.evalArg(n, 0, env, v, func(nv any) error {
			f, ok := toFloat(nv)
			if !ok {
				return fmt.Errorf("Invalid limit: %s", typeDump(nv))
			}
			if f <= 0 {
				if f < 0 {
					return e.run(n.args[1], env, v, p, out)
				}
				return nil
			}
			count := 0
			stop := &stopError{}
			err := e.run(n.args[1], env, v, p, func(x any, xp *pathT) error {
				count++
				if err := out(x, xp); err != nil {
					return err
				}
				if float64(count) >= f {
					return stop
				}
				return nil
			})
			if err == stop {
				return nil
			}
			return err
		})
	})
	defGen("first", 1, func(e *evaluator, n *callNode, env *envT, v any, p *pathT, out emitFunc) error {
		stop := &stopError{}
		err := e.run(n.args[0], env, v, p, func(x any, xp *pathT) error {
			if err := out(x, xp); err != nil {
				return err
			}
			return stop
		})
		if err == stop {
			return nil
		}
		return err
	})
	defGen("isempty", 1, func(e *evaluator, n *callNode, env *envT, v any, p *pathT, out emitFunc) error {
		stop := &stopError{}
		empty := true
		err := e.run(n.args[0], env, v, nil, func(any, *pathT) error {
			empty = false
			return stop
		})
		if err != nil && err != stop {
			return err
		}
		return emitValue(out, empty, p)
	})
	defGen("range", 1, func(e *evaluator, n *callNode, env *envT, v any, p *pathT, out emitFunc) error {
		return e.evalArg(n, 0, env, v, func(upto any) error {
			return e.emitRange(0, upto, 1, p, out)
		})
	})
	defGen("range", 2, func(e *evaluator, n *callNode, env *envT, v any, p *pathT, out emitFunc) error {
		return e.evalArg(n, 0, env, v, func(from any) error {
			return e.evalArg(n, 1, env, v, func(upto any) error {
				return e.emitRange(from, upto, 1, p, out)
			})
		})
	})
	defGen("range", 3, func(e *evaluator, n *callNode, env *envT, v any, p *pathT, out emitFunc) error {
		return e.evalArg(n, 0, env, v, func(from any) error {
			return e.evalArg(n, 1, env, v, func(upto any) error {
				return e.evalArg(n, 2, env, v, func(by any) error {
					return e.emitRange(from, upto, by, p, out)
				})
			})
		})
	})
	defGen("repeat", 1, func(e *evaluator, n *callNode, env *envT, v any, p *pathT, out emitFunc) error {
		// jq 1.7: def repeat(f): def _repeat: f, _repeat; _repeat;
		for {
			if err := e.run(n.args[0], env, v, p, out); err != nil {
				return err
			}
			if err := e.ctx.Err(); err != nil {
				return err
			}
		}
	})
	defGen("while", 2, func(e *evaluator, n *callNode, env *envT, v any, p *pathT, out emitFunc) error {
		var rec func(x any, xp *pathT) error
		rec = func(x any, xp *pathT) error {
			for {
				conds, err := e.collect(n.args[0], env, x, nil)
				if err != nil {
					return err
				}
				if len(conds) != 1 {
					for _, c := range conds {
						if truthy(c.v) {
							if err := out(x, xp); err != nil {
								return err
							}
							if err := e.run(n.args[1], env, x, xp, rec); err != nil {
								return err
							}
						}
					}
					return nil
				}
				if !truthy(conds[0].v) {
					return nil
				}
				if err := out(x, xp); err != nil {
					return err
				}
				next, err := e.collect(n.args[1], env, x, xp)
				if err != nil {
					return err
				}
				if len(next) != 1 {
					for _, y := range next {
						if err := rec(y.v, y.p); err != nil {
							return err
						}
					}
					return nil
				}
				x, xp = next[0].v, next[0].p
			}
		}
		return rec(v, p)
	})
	defGen("until", 2, func(e *evaluator, n *callNode, env *envT, v any, p *pathT, out emitFunc) error {
		var rec func(x any, xp *pathT) error
		rec = func(x any, xp *pathT) error {
			for {
				conds, err := e.collect(n.args[0], env, x, nil)
				if err != nil {
					return err
				}
				if len(conds) != 1 {
					for _, c := range conds {
						if truthy(c.v) {
							if err := out(x, xp); err != nil {
								return err
							}
						} else if err := e.run(n.args[1], env, x, xp, rec); err != nil {
							return err
						}
					}
					return nil
				}
				if truthy(conds[0].v) {
					return out(x, xp)
				}
				next, err := e.collect(n.args[1], env, x, xp)
				if err != nil {
					return err
				}
				if len(next) != 1 {
					for _, y := range next {
						if err := rec(y.v, y.p); err != nil {
							return err
						}
					}
					return nil
				}
				x, xp = next[0].v, next[0].p
			}
		}
		return rec(v, p)
	})
	defFn("input", 0, func(e *evaluator, v any, _ []any) (any, error) {
		return e.nextInput()
	})
	defGen("inputs", 0, func(e *evaluator, n *callNode, env *envT, v any, p *pathT, out emitFunc) error {
		for {
			x, err := e.nextInput()
			if err != nil {
				if err.Error() == "No more inputs" {
					return nil
				}
				return err
			}
			if err := emitValue(out, x, p); err != nil {
				return err
			}
		}
	})
	defFn("input_filename", 0, func(e *evaluator, v any, _ []any) (any, error) {
		if f, ok := e.inputs.(interface{ Filename() any }); ok {
			return f.Filename(), nil
		}
		return nil, nil
	})
	defFn("input_line_number", 0, func(e *evaluator, v any, _ []any) (any, error) { return 0, nil })
	defFn("debug", 0, func(e *evaluator, v any, _ []any) (any, error) {
		if w := e.q.debug; w != nil {
			fmt.Fprintf(w, "[\"DEBUG:\",%s]\n", toJSON(v))
		}
		return v, nil
	})
	defFn("stderr", 0, func(e *evaluator, v any, _ []any) (any, error) {
		if w := e.q.debug; w != nil {
			fmt.Fprint(w, toJSON(v))
		}
		return v, nil
	})
	defFn("halt", 0, func(e *evaluator, v any, _ []any) (any, error) {
		return nil, &HaltError{Code: 0, Value: nil, Silent: true}
	})
	defFn("halt_error", 1, func(e *evaluator, v any, args []any) (any, error) {
		code, ok := args[0].(int)
		if !ok {
			return nil, fmt.Errorf("halt_error/1: number required")
		}
		return nil, &HaltError{Code: code, Value: v}
	})
	defFn("builtins", 0, func(e *evaluator, v any, _ []any) (any, error) {
		out := []any{}
		for k := range globals {
			if !strings.HasPrefix(k, "_") {
				out = append(out, k)
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].(string) < out[j].(string) })
		return out, nil
	})
	defFn("have_decnum", 0, func(*evaluator, any, []any) (any, error) { return false, nil })
	defFn("have_literal_numbers", 0, func(*evaluator, any, []any) (any, error) { return false, nil })

	// ---- values and types ----
	defFn("length", 0, func(e *evaluator, v any, _ []any) (any, error) {
		switch v := v.(type) {
		case nil:
			return 0, nil
		case int:
			if v < 0 {
				if v == math.MinInt {
					return -float64(v), nil
				}
				return -v, nil
			}
			return v, nil
		case float64:
			return math.Abs(v), nil
		case string:
			return runeLen(v), nil
		case []any:
			return len(v), nil
		case map[string]any:
			return len(v), nil
		}
		return nil, fmt.Errorf("%s has no length", typeDump(v))
	})
	defFn("utf8bytelength", 0, func(e *evaluator, v any, _ []any) (any, error) {
		if s, ok := v.(string); ok {
			return len(s), nil
		}
		return nil, fmt.Errorf("%s only strings have UTF-8 byte length", typeDump(v))
	})
	defFn("type", 0, func(e *evaluator, v any, _ []any) (any, error) { return typeName(v), nil })
	defFn("keys", 0, keysFn)
	defFn("keys_unsorted", 0, keysFn)
	defFn("has", 1, func(e *evaluator, v any, args []any) (any, error) {
		switch t := v.(type) {
		case map[string]any:
			if k, ok := args[0].(string); ok {
				_, has := t[k]
				return has, nil
			}
		case []any:
			if f, ok := toFloat(args[0]); ok {
				return f >= 0 && f < float64(len(t)), nil
			}
		}
		return nil, fmt.Errorf("Cannot check whether %s has a %s key", typeName(v), typeName(args[0]))
	})
	defFn("contains", 1, func(e *evaluator, v any, args []any) (any, error) {
		return contains(v, args[0])
	})
	defFn("add", 0, func(e *evaluator, v any, _ []any) (any, error) { return addAll(v) })
	defFn("tostring", 0, func(e *evaluator, v any, _ []any) (any, error) { return toString(v), nil })
	defFn("tojson", 0, func(e *evaluator, v any, _ []any) (any, error) { return toJSON(v), nil })
	defFn("fromjson", 0, func(e *evaluator, v any, _ []any) (any, error) {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("%s cannot be parsed as JSON", typeDump(v))
		}
		r, err := parseJSON([]byte(s))
		if err != nil {
			return nil, fmt.Errorf("%s (while parsing '%s')", err, s)
		}
		return r, nil
	})
	defFn("tonumber", 0, func(e *evaluator, v any, _ []any) (any, error) {
		switch x := v.(type) {
		case int, float64:
			return v, nil
		case string:
			t := strings.TrimSpace(x)
			if validNumber(t) {
				return parseNumber(t)
			}
			switch t {
			case "nan", "NaN":
				return nan, nil
			}
			return nil, fmt.Errorf("Cannot parse '%s' as a number", x)
		}
		return nil, fmt.Errorf("%s cannot be parsed as a number", typeDump(v))
	})
	defFn("infinite", 0, func(*evaluator, any, []any) (any, error) { return math.Inf(1), nil })
	defFn("nan", 0, func(*evaluator, any, []any) (any, error) { return nan, nil })
	defFn("isinfinite", 0, numPred(func(f float64) bool { return math.IsInf(f, 0) }))
	defFn("isnan", 0, numPred(math.IsNaN))
	defFn("isnormal", 0, numPred(func(f float64) bool {
		if math.IsNaN(f) || math.IsInf(f, 0) || f == 0 {
			return false
		}
		return math.Abs(f) >= 2.2250738585072014e-308
	}))
	defFn("abs", 0, func(e *evaluator, v any, _ []any) (any, error) {
		switch x := v.(type) {
		case int:
			if x < 0 {
				if x == math.MinInt {
					return -float64(x), nil
				}
				return -x, nil
			}
			return x, nil
		case float64:
			if x < 0 {
				return -x, nil
			}
			return x, nil
		case nil, bool:
			return nil, fmt.Errorf("%s cannot be negated", typeDump(v))
		}
		return v, nil
	})

	// ---- arrays ----
	defFn("sort", 0, func(e *evaluator, v any, _ []any) (any, error) {
		arr, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("%s cannot be sorted, as it is not an array", typeDump(v))
		}
		out := cloneSlice(arr, 0)
		sort.SliceStable(out, func(i, j int) bool { return compare(out[i], out[j]) < 0 })
		return out, nil
	})
	defFn("_sort_by_impl", 1, func(e *evaluator, v any, args []any) (any, error) {
		arr, keys, err := byImplArgs(v, args[0], "sorted")
		if err != nil {
			return nil, err
		}
		idx := sortedIndex(keys)
		out := make([]any, len(arr))
		for i, j := range idx {
			out[i] = arr[j]
		}
		return out, nil
	})
	defFn("_group_by_impl", 1, func(e *evaluator, v any, args []any) (any, error) {
		arr, keys, err := byImplArgs(v, args[0], "grouped")
		if err != nil {
			return nil, err
		}
		out := []any{}
		idx := sortedIndex(keys)
		for i := 0; i < len(idx); {
			j := i + 1
			for j < len(idx) && compare(keys[idx[i]], keys[idx[j]]) == 0 {
				j++
			}
			group := make([]any, 0, j-i)
			for _, k := range idx[i:j] {
				group = append(group, arr[k])
			}
			out = append(out, group)
			i = j
		}
		return out, nil
	})
	defFn("_unique_by_impl", 1, func(e *evaluator, v any, args []any) (any, error) {
		arr, keys, err := byImplArgs(v, args[0], "sorted")
		if err != nil {
			return nil, err
		}
		out := []any{}
		idx := sortedIndex(keys)
		for i := 0; i < len(idx); i++ {
			if i > 0 && compare(keys[idx[i-1]], keys[idx[i]]) == 0 {
				continue
			}
			out = append(out, arr[idx[i]])
		}
		return out, nil
	})
	defFn("_min_by_impl", 1, func(e *evaluator, v any, args []any) (any, error) {
		return extremeBy(v, args[0], false)
	})
	defFn("_max_by_impl", 1, func(e *evaluator, v any, args []any) (any, error) {
		return extremeBy(v, args[0], true)
	})
	defFn("unique", 0, func(e *evaluator, v any, _ []any) (any, error) {
		arr, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("%s cannot be sorted, as it is not an array", typeDump(v))
		}
		s := cloneSlice(arr, 0)
		sort.SliceStable(s, func(i, j int) bool { return compare(s[i], s[j]) < 0 })
		out := []any{}
		for i, x := range s {
			if i > 0 && compare(s[i-1], x) == 0 {
				continue
			}
			out = append(out, x)
		}
		return out, nil
	})
	defFn("min", 0, func(e *evaluator, v any, _ []any) (any, error) { return extremeBy(v, v, false) })
	defFn("max", 0, func(e *evaluator, v any, _ []any) (any, error) { return extremeBy(v, v, true) })
	defFn("reverse", 0, func(e *evaluator, v any, _ []any) (any, error) {
		switch x := v.(type) {
		case nil:
			return []any{}, nil
		case []any:
			out := make([]any, len(x))
			for i, el := range x {
				out[len(x)-1-i] = el
			}
			return out, nil
		case string:
			if x == "" {
				return []any{}, nil
			}
		}
		return nil, fmt.Errorf("Cannot index %s with number", typeName(v))
	})
	defFn("flatten", 0, func(e *evaluator, v any, _ []any) (any, error) { return flatten(v, 1e9) })
	defFn("flatten", 1, func(e *evaluator, v any, args []any) (any, error) {
		d, ok := toFloat(args[0])
		if !ok {
			return nil, fmt.Errorf("flatten depth must not be negative")
		}
		if d < 0 {
			return nil, fmt.Errorf("flatten depth must not be negative")
		}
		return flatten(v, d)
	})
	defFn("indices", 1, func(e *evaluator, v any, args []any) (any, error) { return indicesOf(v, args[0]) })
	defFn("index", 1, func(e *evaluator, v any, args []any) (any, error) {
		r, err := indicesOf(v, args[0])
		if arr, ok := r.([]any); ok {
			if len(arr) == 0 {
				return nil, err
			}
			return arr[0], err
		}
		return r, err
	})
	defFn("rindex", 1, func(e *evaluator, v any, args []any) (any, error) {
		r, err := indicesOf(v, args[0])
		if arr, ok := r.([]any); ok {
			if len(arr) == 0 {
				return nil, err
			}
			return arr[len(arr)-1], err
		}
		return r, err
	})
	defFn("to_entries", 0, func(e *evaluator, v any, _ []any) (any, error) {
		switch x := v.(type) {
		case map[string]any:
			out := make([]any, 0, len(x))
			for _, k := range sortedKeys(x) {
				out = append(out, map[string]any{"key": k, "value": x[k]})
			}
			return out, nil
		case []any:
			out := make([]any, len(x))
			for i, el := range x {
				out[i] = map[string]any{"key": i, "value": el}
			}
			return out, nil
		}
		return nil, fmt.Errorf("%s has no keys", typeDump(v))
	})
	defFn("from_entries", 0, func(e *evaluator, v any, _ []any) (any, error) {
		arr, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("Cannot iterate over %s", typeDump(v))
		}
		out := make(map[string]any, len(arr))
		for _, ent := range arr {
			m, ok := ent.(map[string]any)
			if !ok {
				if ent == nil {
					return nil, fmt.Errorf("Cannot use null (null) as object key")
				}
				return nil, fmt.Errorf("Cannot index %s with \"key\"", typeName(ent))
			}
			var key any
			for _, name := range []string{"key", "Key", "name", "Name"} {
				if k := m[name]; truthy(k) {
					key = k
					break
				}
			}
			ks, ok := key.(string)
			if !ok {
				if key == nil {
					return nil, fmt.Errorf("Cannot use null (null) as object key")
				}
				return nil, fmt.Errorf("Object keys must be strings")
			}
			if val, ok := m["value"]; ok {
				out[ks] = val
			} else {
				out[ks] = m["Value"]
			}
		}
		return out, nil
	})

	// ---- strings ----
	defFn("explode", 0, func(e *evaluator, v any, _ []any) (any, error) {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("%s cannot be exploded, as it is not a string", typeDump(v))
		}
		out := make([]any, 0, len(s))
		for _, r := range s {
			out = append(out, int(r))
		}
		return out, nil
	})
	defFn("implode", 0, func(e *evaluator, v any, _ []any) (any, error) {
		arr, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("implode input must be an array")
		}
		var sb strings.Builder
		for _, x := range arr {
			f, ok := toFloat(x)
			if !ok || math.IsNaN(f) {
				if !ok {
					return nil, fmt.Errorf("%s can't be imploded, unicode codepoint needs to be numeric", typeDump(x))
				}
				return nil, fmt.Errorf("number (null) can't be imploded, unicode codepoint needs to be numeric")
			}
			r := rune(f)
			if f > utf8.MaxRune || f < 0 || (r >= 0xd800 && r < 0xe000) {
				r = utf8.RuneError
			}
			sb.WriteRune(r)
		}
		return sb.String(), nil
	})
	defFn("ltrimstr", 1, func(e *evaluator, v any, args []any) (any, error) {
		s, ok1 := v.(string)
		pre, ok2 := args[0].(string)
		if ok1 && ok2 && strings.HasPrefix(s, pre) {
			return s[len(pre):], nil
		}
		return v, nil
	})
	defFn("rtrimstr", 1, func(e *evaluator, v any, args []any) (any, error) {
		s, ok1 := v.(string)
		suf, ok2 := args[0].(string)
		if ok1 && ok2 && strings.HasSuffix(s, suf) && len(suf) > 0 {
			return s[:len(s)-len(suf)], nil
		}
		return v, nil
	})
	defFn("startswith", 1, func(e *evaluator, v any, args []any) (any, error) {
		s, ok1 := v.(string)
		pre, ok2 := args[0].(string)
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("startswith() requires string inputs")
		}
		return strings.HasPrefix(s, pre), nil
	})
	defFn("endswith", 1, func(e *evaluator, v any, args []any) (any, error) {
		s, ok1 := v.(string)
		suf, ok2 := args[0].(string)
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("endswith() requires string inputs")
		}
		return strings.HasSuffix(s, suf), nil
	})
	trim := func(name string, f func(string) string) {
		defFn(name, 0, func(e *evaluator, v any, _ []any) (any, error) {
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("%s input must be a string", name)
			}
			return f(s), nil
		})
	}
	trim("trim", func(s string) string { return strings.TrimSpace(s) })
	trim("ltrim", func(s string) string { return strings.TrimLeft(s, " \t\n\r\f\v") })
	trim("rtrim", func(s string) string { return strings.TrimRight(s, " \t\n\r\f\v") })
	defFn("ascii_downcase", 0, func(e *evaluator, v any, _ []any) (any, error) {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("ascii_downcase input must be a string")
		}
		return asciiMap(s, 'A', 'Z', 'a'-'A'), nil
	})
	defFn("ascii_upcase", 0, func(e *evaluator, v any, _ []any) (any, error) {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("ascii_upcase input must be a string")
		}
		return asciiMap(s, 'a', 'z', 'A'-'a'), nil
	})
	defFn("split", 1, func(e *evaluator, v any, args []any) (any, error) {
		s, ok1 := v.(string)
		sep, ok2 := args[0].(string)
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("split input and separator must be strings")
		}
		return splitString(s, sep), nil
	})
	defFn("join", 1, func(e *evaluator, v any, args []any) (any, error) { return join(v, args[0]) })
	defFn("format", 1, func(e *evaluator, v any, args []any) (any, error) {
		name, ok := args[0].(string)
		if !ok {
			return nil, fmt.Errorf("%s is not a valid format", typeDump(args[0]))
		}
		return applyFormat(name, v)
	})
	defFn("bsearch", 1, func(e *evaluator, v any, args []any) (any, error) {
		arr, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("%s cannot be searched from", typeDump(v))
		}
		i := sort.Search(len(arr), func(i int) bool { return compare(arr[i], args[0]) >= 0 })
		if i < len(arr) && compare(arr[i], args[0]) == 0 {
			return i, nil
		}
		return -1 - i, nil
	})
	defFn("_match_impl", 3, matchImpl)
	defFn("split", 2, splitRegex)
	defGen("sub", 3, subImpl)

	// ---- environment ----
	defFn("now", 0, func(*evaluator, any, []any) (any, error) { return nowFloat(), nil })
	defFn("mktime", 0, mktime)
	defFn("gmtime", 0, func(e *evaluator, v any, _ []any) (any, error) { return brokenDown(v, false) })
	defFn("localtime", 0, func(e *evaluator, v any, _ []any) (any, error) { return brokenDown(v, true) })
	defFn("strftime", 1, func(e *evaluator, v any, args []any) (any, error) { return strftimeFn(v, args[0], false) })
	defFn("strflocaltime", 1, func(e *evaluator, v any, args []any) (any, error) { return strftimeFn(v, args[0], true) })
	defFn("strptime", 1, strptimeFn)

	registerMath()
}

func keysFn(e *evaluator, v any, _ []any) (any, error) {
	switch x := v.(type) {
	case map[string]any:
		keys := sortedKeys(x)
		out := make([]any, len(keys))
		for i, k := range keys {
			out[i] = k
		}
		return out, nil
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = i
		}
		return out, nil
	}
	return nil, fmt.Errorf("%s has no keys", typeDump(v))
}

func numPred(f func(float64) bool) valueFunc {
	return func(e *evaluator, v any, _ []any) (any, error) {
		x, ok := toFloat(v)
		if !ok {
			return nil, fmt.Errorf("%s number required", typeDump(v))
		}
		return f(x), nil
	}
}

func asciiMap(s string, lo, hi byte, delta int) string {
	b := []byte(s)
	for i, c := range b {
		if c >= lo && c <= hi {
			b[i] = byte(int(c) + delta)
		}
	}
	return string(b)
}

type pv struct {
	v any
	p *pathT
}

// collect gathers every output of n (with paths when p != nil).
func (e *evaluator) collect(n node, env *envT, v any, p *pathT) ([]pv, error) {
	var out []pv
	err := e.run(n, env, v, p, func(x any, xp *pathT) error {
		out = append(out, pv{x, xp})
		return nil
	})
	return out, err
}

// emitRange produces range($from; $upto; $by). Its loop may emit straight
// into a collector ([range(1e18)]) without re-entering run, so it checks
// for cancellation itself.
func (e *evaluator) emitRange(from, upto, by any, p *pathT, out emitFunc) error {
	for _, x := range []any{from, upto, by} {
		if !isNumber(x) {
			return fmt.Errorf("Range bounds must be numeric")
		}
	}
	n := 0
	emit := func(x any) error {
		if n++; n&0xfff == 0 {
			if err := e.ctx.Err(); err != nil {
				return err
			}
		}
		return emitValue(out, x, p)
	}
	fi, ok1 := from.(int)
	ui, ok2 := upto.(int)
	bi, ok3 := by.(int)
	if ok1 && ok2 && ok3 {
		switch {
		case bi > 0:
			for x := fi; x < ui; x += bi {
				if err := emit(x); err != nil {
					return err
				}
				if x > math.MaxInt-bi {
					break
				}
			}
		case bi < 0:
			for x := fi; x > ui; x += bi {
				if err := emit(x); err != nil {
					return err
				}
				if x < math.MinInt-bi {
					break
				}
			}
		}
		return nil
	}
	f, _ := toFloat(from)
	u, _ := toFloat(upto)
	b, _ := toFloat(by)
	switch {
	case b > 0:
		for x := f; x < u; x += b {
			if err := emit(intIfExactNum(x, from, by)); err != nil {
				return err
			}
			if x+b == x {
				break // step too small to make progress
			}
		}
	case b < 0:
		for x := f; x > u; x += b {
			if err := emit(intIfExactNum(x, from, by)); err != nil {
				return err
			}
			if x+b == x {
				break
			}
		}
	}
	return nil
}

// intIfExactNum keeps range(0; 2.5) producing ints 0, 1, 2 when the start
// and step are integers.
func intIfExactNum(x float64, from, by any) any {
	_, fi := from.(int)
	_, bi := by.(int)
	if fi && bi {
		return intIfExact(x)
	}
	return x
}

func contains(a, b any) (any, error) {
	if kindOrder(a) != kindOrder(b) && !(isBool(a) && isBool(b)) {
		return nil, fmt.Errorf("%s and %s cannot have their containment checked", typeDump(a), typeDump(b))
	}
	switch x := a.(type) {
	case map[string]any:
		y := b.(map[string]any)
		for k, bv := range y {
			av, ok := x[k]
			if !ok {
				return false, nil
			}
			c, err := contains(av, bv)
			if err != nil {
				return nil, err
			}
			if c != true {
				return false, nil
			}
		}
		return true, nil
	case []any:
		y := b.([]any)
		for _, bv := range y {
			found := false
			for _, av := range x {
				if kindOrder(av) != kindOrder(bv) {
					continue
				}
				c, err := contains(av, bv)
				if err != nil {
					return nil, err
				}
				if c == true {
					found = true
					break
				}
			}
			if !found {
				return false, nil
			}
		}
		return true, nil
	case string:
		return strings.Contains(x, b.(string)), nil
	}
	return equal(a, b), nil
}

func isBool(v any) bool { _, ok := v.(bool); return ok }

func addAll(v any) (any, error) {
	var items []any
	switch x := v.(type) {
	case nil:
		return nil, nil
	case []any:
		items = x
	case map[string]any:
		for _, k := range sortedKeys(x) {
			items = append(items, x[k])
		}
	default:
		return nil, fmt.Errorf("Cannot iterate over %s", typeDump(v))
	}
	// Fast paths avoid quadratic concatenation.
	kind := -1
	for _, it := range items {
		if it == nil {
			continue
		}
		k := kindOrder(it)
		if kind == -1 {
			kind = k
		} else if kind != k {
			kind = -2
			break
		}
	}
	switch kind {
	case 4: // strings
		var sb strings.Builder
		for _, it := range items {
			if s, ok := it.(string); ok {
				sb.WriteString(s)
			}
		}
		return sb.String(), nil
	case 5: // arrays
		out := []any{}
		for _, it := range items {
			if a, ok := it.([]any); ok {
				out = append(out, a...)
			}
		}
		return out, nil
	case 6: // objects
		out := map[string]any{}
		for _, it := range items {
			if m, ok := it.(map[string]any); ok {
				for k, val := range m {
					out[k] = val
				}
			}
		}
		return out, nil
	}
	var acc any
	for _, it := range items {
		var err error
		if acc, err = add(acc, it); err != nil {
			return nil, err
		}
	}
	return acc, nil
}

func byImplArgs(v, keys any, verb string) ([]any, []any, error) {
	arr, ok := v.([]any)
	if !ok {
		return nil, nil, fmt.Errorf("Cannot index %s with number", typeName(v))
	}
	ks, ok := keys.([]any)
	if !ok || len(ks) != len(arr) {
		return nil, nil, fmt.Errorf("%s cannot be %s, as it is not an array", typeDump(v), verb)
	}
	return arr, ks, nil
}

func sortedIndex(keys []any) []int {
	idx := make([]int, len(keys))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(i, j int) bool { return compare(keys[idx[i]], keys[idx[j]]) < 0 })
	return idx
}

// extremeBy returns the element with the smallest (first one wins) or
// largest (last one wins) key, as jq's min_by/max_by do.
func extremeBy(v, keys any, max bool) (any, error) {
	arr, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("%s cannot be sorted, as it is not an array", typeDump(v))
	}
	ks, ok := keys.([]any)
	if !ok || len(ks) != len(arr) {
		return nil, fmt.Errorf("%s cannot be sorted, as it is not an array", typeDump(v))
	}
	if len(arr) == 0 {
		return nil, nil
	}
	best := 0
	for i := 1; i < len(arr); i++ {
		c := compare(ks[i], ks[best])
		if (max && c >= 0) || (!max && c < 0) {
			best = i
		}
	}
	return arr[best], nil
}

func flatten(v any, depth float64) (any, error) {
	arr, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("Cannot iterate over %s", typeDump(v))
	}
	out := []any{}
	var rec func(a []any, d float64)
	rec = func(a []any, d float64) {
		for _, x := range a {
			if sub, ok := x.([]any); ok && d > 0 {
				rec(sub, d-1)
			} else {
				out = append(out, x)
			}
		}
	}
	rec(arr, depth)
	return out, nil
}

func indicesOf(v, x any) (any, error) {
	switch t := v.(type) {
	case nil:
		return nil, nil
	case string:
		sub, ok := x.(string)
		if !ok {
			break
		}
		out := []any{}
		if sub == "" {
			return nil, nil
		}
		ascii := isASCII(t)
		for i := 0; i+len(sub) <= len(t); {
			j := strings.Index(t[i:], sub)
			if j < 0 {
				break
			}
			pos := i + j
			if ascii {
				out = append(out, pos)
			} else {
				out = append(out, utf8.RuneCountInString(t[:pos]))
			}
			_, size := utf8.DecodeRuneInString(t[pos:])
			i = pos + size
		}
		return out, nil
	case []any:
		if sub, ok := x.([]any); ok {
			return arrayIndices(t, sub), nil
		}
		return arrayIndices(t, []any{x}), nil
	}
	return index(v, x)
}

func join(v, sep any) (any, error) {
	arr, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("Cannot iterate over %s", typeDump(v))
	}
	if len(arr) == 0 {
		return "", nil
	}
	s, ok := sep.(string)
	if !ok {
		// jq builds the result with +, so the error comes from there.
		if _, err := add("", sep); err != nil {
			return nil, err
		}
	}
	var sb strings.Builder
	for i, x := range arr {
		if i > 0 {
			sb.WriteString(s)
		}
		switch x := x.(type) {
		case nil:
		case string:
			sb.WriteString(x)
		case bool, int, float64:
			sb.WriteString(toJSON(x))
		default:
			acc := sb.String()
			return nil, fmt.Errorf("%s and %s cannot be added", typeDump(acc), typeDump(x))
		}
	}
	return sb.String(), nil
}
