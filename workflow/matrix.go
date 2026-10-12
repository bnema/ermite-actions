// SPDX-License-Identifier: MIT

package workflow

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"bnema.dev/ermite-actions/expr"
)

// MaxMatrixJobs is the number of jobs a matrix may produce.
const MaxMatrixJobs = 256

// maxMatrixProduct bounds the size of the cross product before exclusions,
// so that a matrix of many large vectors fails fast instead of looping.
// GitHub only limits the final number of jobs.
const maxMatrixProduct = 1 << 20

// MatrixCombo is one job of a matrix.
type MatrixCombo struct {
	// Values is the "matrix" context of the job: the vector values in the
	// order of the keys, then the keys added by include entries in the order
	// they were first added.
	Values *expr.Dictionary
	// Segments are the scalar values of Values, in order, shown in the name
	// of the job: "build (ubuntu-latest, 14)". Arrays and mappings are left
	// out.
	Segments []string
}

// DisplayName returns the name of the job: base alone when there are no
// segments, else base followed by the segments in parentheses.
func (c MatrixCombo) DisplayName(base string) string {
	if len(c.Segments) == 0 {
		return base
	}
	return base + " (" + strings.Join(c.Segments, ", ") + ")"
}

// Strategy is an evaluated "strategy" key.
type Strategy struct {
	// FailFast is true unless the file says otherwise.
	FailFast bool
	// MaxParallel is the limit of concurrent jobs, 0 for no limit.
	MaxParallel int
	// HasMatrix reports whether the strategy has a matrix. Without one,
	// Combos holds a single empty combination: the job runs once.
	HasMatrix bool
	// Combos are the jobs, in the order GitHub creates them.
	Combos []MatrixCombo
}

// ExpandMatrix expands the matrix of an evaluated strategy (a dictionary with
// the optional keys "matrix", "fail-fast" and "max-parallel", or null) into
// the job combinations, like GitHub:
//
//   - the combinations are the cross product of the vectors, the first key
//     varying slowest;
//   - exclude removes every combination that contains all the key-value
//     pairs of one of its entries;
//   - then each include entry is added to every remaining combination whose
//     values for the vector keys it mentions are equal (an entry that
//     mentions no vector key matches all), without overwriting the original
//     values; an entry that matches no combination becomes a combination of
//     its own;
//   - more than 256 jobs is an error. A strategy without a matrix gives a
//     single combination.
//
// TODO(verify with recording): the documented semantics of include and
// exclude are implemented as documented, but the following are assumed: the
// order of the cross product (first key slowest), the display name listing
// every scalar value including those added by include (see
// [MatrixCombo.DisplayName]), the comparison of values (same kind, strings
// compared exactly, mappings by containment), an include key matching a
// vector key case-insensitively, the error when every combination is
// excluded, and the wording of all errors.
func ExpandMatrix(strategy expr.Value) ([]MatrixCombo, error) {
	s, err := ParseStrategy(strategy)
	if err != nil {
		return nil, err
	}
	return s.Combos, nil
}

// ParseStrategy reads an evaluated strategy value and expands its matrix.
func ParseStrategy(v expr.Value) (*Strategy, error) {
	s := &Strategy{FailFast: true}
	var matrix expr.Value
	switch d := v.(type) {
	case nil, expr.Null:
	case *expr.Dictionary:
		if d == nil {
			break
		}
		for _, p := range d.Pairs() {
			switch strings.ToLower(p.Key) {
			case "fail-fast":
				b, err := strategyBool(p.Value)
				if err != nil {
					return nil, fmt.Errorf("Unexpected value for 'fail-fast': %w", err)
				}
				s.FailFast = b
			case "max-parallel":
				n, err := strategyInt(p.Value)
				if err != nil {
					return nil, fmt.Errorf("Unexpected value for 'max-parallel': %w", err)
				}
				s.MaxParallel = n
			case "matrix":
				matrix = p.Value
			}
		}
	default:
		return nil, errors.New("The strategy must be a mapping")
	}
	combos, has, err := buildMatrix(matrix)
	if err != nil {
		return nil, err
	}
	s.Combos, s.HasMatrix = combos, has
	return s, nil
}

// Context returns the "strategy" context of job number index (from 0).
func (s *Strategy) Context(index int) *expr.Dictionary {
	d := expr.NewDictionary()
	d.Add("fail-fast", expr.Boolean(s.FailFast))
	d.Add("job-index", expr.Number(index))
	d.Add("job-total", expr.Number(len(s.Combos)))
	max := s.MaxParallel
	if max == 0 {
		max = len(s.Combos)
	}
	d.Add("max-parallel", expr.Number(max))
	return d
}

func strategyBool(v expr.Value) (bool, error) {
	switch x := v.(type) {
	case expr.Boolean:
		return bool(x), nil
	case expr.String:
		switch strings.ToLower(string(x)) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
	}
	return false, fmt.Errorf("expected a boolean but got %s", kindName(v))
}

func strategyInt(v expr.Value) (int, error) {
	var f float64
	switch x := v.(type) {
	case expr.Number:
		f = float64(x)
	case expr.String:
		n, err := strconv.ParseFloat(strings.TrimSpace(string(x)), 64)
		if err != nil {
			return 0, fmt.Errorf("expected a number but got '%s'", string(x))
		}
		f = n
	default:
		return 0, fmt.Errorf("expected a number but got %s", kindName(v))
	}
	if f != float64(int(f)) || f < 1 || f > 1<<31 {
		return 0, errors.New("expected a whole number greater than 0")
	}
	return int(f), nil
}

func kindName(v expr.Value) string {
	if v == nil {
		return expr.KindNull.String()
	}
	return v.Kind().String()
}

func buildMatrix(m expr.Value) ([]MatrixCombo, bool, error) {
	switch m.(type) {
	case nil, expr.Null:
		return []MatrixCombo{{Values: expr.NewDictionary()}}, false, nil
	}
	md, ok := m.(*expr.Dictionary)
	if !ok {
		return nil, false, errors.New("The matrix must be a mapping")
	}
	if md == nil {
		return []MatrixCombo{{Values: expr.NewDictionary()}}, false, nil
	}
	var keys []string
	vectors := map[string][]expr.Value{}
	var includes, excludes []*expr.Dictionary
	for _, p := range md.Pairs() {
		switch p.Key {
		case "include":
			list, err := matrixEntries("include", p.Value)
			if err != nil {
				return nil, false, err
			}
			includes = list
		case "exclude":
			list, err := matrixEntries("exclude", p.Value)
			if err != nil {
				return nil, false, err
			}
			excludes = list
		default:
			arr, ok := p.Value.(*expr.Array)
			if !ok || arr == nil {
				return nil, false, fmt.Errorf("Matrix vector '%s' does not contain any values", p.Key)
			}
			if arr.Len() == 0 {
				return nil, false, fmt.Errorf("Matrix vector '%s' does not contain any values", p.Key)
			}
			keys = append(keys, p.Key)
			vectors[p.Key] = arr.Items()
		}
	}
	return expand(keys, vectors, includes, excludes)
}

func matrixEntries(name string, v expr.Value) ([]*expr.Dictionary, error) {
	arr, ok := v.(*expr.Array)
	if !ok || arr == nil {
		return nil, fmt.Errorf("Matrix %s must be a sequence of mappings", name)
	}
	var out []*expr.Dictionary
	for _, it := range arr.Items() {
		d, ok := it.(*expr.Dictionary)
		if !ok || d == nil {
			return nil, fmt.Errorf("Matrix %s must be a sequence of mappings", name)
		}
		out = append(out, d)
	}
	return out, nil
}

// comboBuilder holds the values of one combination while includes are
// applied; added values may be overwritten by later includes, original ones
// never are.
type comboBuilder struct {
	keys []string
	vals map[string]expr.Value // by lower-cased key
}

func (b *comboBuilder) set(key string, v expr.Value) {
	lk := strings.ToLower(key)
	if _, ok := b.vals[lk]; !ok {
		b.keys = append(b.keys, key)
	}
	b.vals[lk] = v
}

func (b *comboBuilder) combo() MatrixCombo {
	d := expr.NewDictionary()
	var segs []string
	for _, k := range b.keys {
		v := b.vals[strings.ToLower(k)]
		d.Add(k, v)
		if s, ok := segment(v); ok {
			segs = append(segs, s)
		}
	}
	return MatrixCombo{Values: d, Segments: segs}
}

func expand(keys []string, vectors map[string][]expr.Value, includes, excludes []*expr.Dictionary) ([]MatrixCombo, bool, error) {
	var combos []MatrixCombo
	matched := make([]bool, len(includes))
	tooBig := fmt.Errorf("Matrix size limit of %d exceeded", MaxMatrixJobs)
	if len(keys) > 0 {
		product := 1
		for _, k := range keys {
			product *= len(vectors[k])
			if product > maxMatrixProduct {
				return nil, false, tooBig
			}
		}
		for i := 0; i < product; i++ {
			b := &comboBuilder{vals: map[string]expr.Value{}}
			div := product
			for _, k := range keys {
				div /= len(vectors[k])
				b.set(k, vectors[k][(i/div)%len(vectors[k])])
			}
			base := b.combo().Values
			if excluded(base, excludes) {
				continue
			}
			for n, inc := range includes {
				if !includeMatches(base, keys, inc) {
					continue
				}
				matched[n] = true
				for _, p := range inc.Pairs() {
					if _, isVector := vectors[p.Key]; !isVector && !hasKeyFold(keys, p.Key) {
						b.set(p.Key, p.Value)
					}
				}
			}
			combos = append(combos, b.combo())
			if len(combos) > MaxMatrixJobs {
				return nil, false, tooBig
			}
		}
	}
	for n, inc := range includes {
		if matched[n] {
			continue
		}
		b := &comboBuilder{vals: map[string]expr.Value{}}
		for _, p := range inc.Pairs() {
			b.set(p.Key, p.Value)
		}
		combos = append(combos, b.combo())
		if len(combos) > MaxMatrixJobs {
			return nil, false, tooBig
		}
	}
	if len(combos) == 0 {
		if len(keys) == 0 && len(includes) == 0 && len(excludes) == 0 {
			// An empty matrix runs the job once.
			return []MatrixCombo{{Values: expr.NewDictionary()}}, true, nil
		}
		// TODO(verify with recording): GitHub's behaviour when every
		// combination is excluded.
		return nil, false, errors.New("The matrix has no combinations: all of them are excluded")
	}
	return combos, true, nil
}

func hasKeyFold(keys []string, key string) bool {
	for _, k := range keys {
		if strings.EqualFold(k, key) {
			return true
		}
	}
	return false
}

// excluded reports whether any exclude entry is contained in vals.
func excluded(vals *expr.Dictionary, excludes []*expr.Dictionary) bool {
	for _, ex := range excludes {
		if ex.Len() == 0 {
			continue
		}
		all := true
		for _, p := range ex.Pairs() {
			have, ok := vals.Get(p.Key)
			if !ok || !valuesMatch(have, p.Value) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

// includeMatches reports whether an include entry applies to vals: every key
// of the entry that is a vector of the matrix has the same value. An entry
// with no such key applies to all combinations.
func includeMatches(vals *expr.Dictionary, vectorKeys []string, inc *expr.Dictionary) bool {
	for _, p := range inc.Pairs() {
		if !hasKeyFold(vectorKeys, p.Key) {
			continue
		}
		have, _ := vals.Get(p.Key)
		if !valuesMatch(have, p.Value) {
			return false
		}
	}
	return true
}

// valuesMatch compares a matrix value with the value in an include or
// exclude entry: scalars of the same kind and value are equal, arrays must be
// equal element by element, and a mapping in the entry must be contained in
// the matrix value.
func valuesMatch(have, want expr.Value) bool {
	if kindName(have) != kindName(want) {
		return false
	}
	switch w := want.(type) {
	case expr.Null:
		return true
	case expr.Boolean:
		return have.(expr.Boolean) == w
	case expr.Number:
		return have.(expr.Number) == w
	case expr.String:
		return have.(expr.String) == w
	case *expr.Array:
		h := have.(*expr.Array)
		if h.Len() != w.Len() {
			return false
		}
		for i := 0; i < w.Len(); i++ {
			if !valuesMatch(h.At(i), w.At(i)) {
				return false
			}
		}
		return true
	case *expr.Dictionary:
		h := have.(*expr.Dictionary)
		for _, p := range w.Pairs() {
			hv, ok := h.Get(p.Key)
			if !ok || !valuesMatch(hv, p.Value) {
				return false
			}
		}
		return true
	}
	return false
}

// segment is the text a matrix value adds to the name of a job; arrays and
// mappings add nothing.
func segment(v expr.Value) (string, bool) {
	switch v.(type) {
	case *expr.Array, *expr.Dictionary:
		return "", false
	}
	return expr.CoerceString(v), true
}
