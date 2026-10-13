// SPDX-License-Identifier: MIT

package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"bnema.dev/ermite-actions/expr"
)

// Limits of matrix expansion. GitHub only limits the number of jobs; the
// other limits keep the work done for a hostile matrix bounded.
const (
	// MaxMatrixJobs is the number of jobs a matrix may produce.
	MaxMatrixJobs = 256
	// MaxMatrixProduct is the size of the cross product of the vectors, before
	// exclusions, above which a matrix is refused.
	MaxMatrixProduct = 1 << 16
	// MaxMatrixEntries is the number of entries of include and, separately,
	// of exclude.
	MaxMatrixEntries = 256
)

// checkEvery is the number of combinations between two checks of the context.
const checkEvery = 1024

// MatrixCombo is one job of a matrix.
type MatrixCombo struct {
	// Values is the "matrix" context of the job: the vector values in the
	// order of the keys, then the keys added by include entries in the order
	// they were first added.
	Values *expr.Dictionary
	// Segments are the scalar values of Values, in order, shown in the name
	// of the job: "build (ubuntu-latest, 14)". Null and the empty string are
	// left out, as recorded (matrix-edge-values); arrays and mappings are
	// left out too, which is assumed.
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

// ExpandMatrix expands the matrix of an evaluated strategy (a dictionary with
// the optional keys "matrix", "fail-fast" and "max-parallel", or null) into
// the job combinations, like GitHub:
//
//   - the combinations are the cross product of the vectors, the first key
//     varying slowest;
//   - exclude removes every combination that contains all the key-value
//     pairs of one of its entries; every key of an entry must be a vector of
//     the matrix, or the matrix is an error that matches
//     [ErrExcludeUnknownKey];
//   - then each include entry is added to every remaining combination whose
//     values for the vector keys it mentions are equal (an entry that
//     mentions no vector key matches all), without overwriting the original
//     values; an entry that matches no combination becomes a combination of
//     its own;
//   - more than 256 jobs is an error. A strategy without a matrix gives a
//     single combination.
//
// The work is bounded: the product of the vectors may not exceed
// [MaxMatrixProduct], include and exclude may have [MaxMatrixEntries] entries
// each, and ctx is checked every few thousand combinations.
//
// What github.com settled. Recorded (matrix-edge-values): null and the empty
// string are left out of the default job name. For matrix-edge-values (exclude key not
// in the matrix) and matrix-over-256 the recordings hold only a failed run
// with no job: the wording of the two errors below, and the unknown exclude
// key being reported once per offending entry, were observed on the github.com
// run page, not in the recordings. Still assumed, from the documented
// semantics of include and exclude: the order of the cross product (first key
// slowest), the display name listing every scalar value including those
// added by include (see [MatrixCombo.DisplayName]), the comparison of values
// (same kind, strings compared exactly, mappings by containment), an include
// key matching a vector key case-insensitively, the error when every
// combination is excluded, the other limits above, and the wording of the
// other errors.
func ExpandMatrix(ctx context.Context, strategy expr.Value) ([]MatrixCombo, error) {
	s, err := ParseStrategy(ctx, strategy)
	if err != nil {
		return nil, err
	}
	return s.Combos, nil
}

func buildMatrix(ctx context.Context, m expr.Value) ([]MatrixCombo, bool, error) {
	switch m.(type) {
	case nil, expr.Null:
		return []MatrixCombo{{Values: expr.NewDictionary()}}, false, nil
	}
	md, ok := m.(*expr.Dictionary)
	if !ok {
		return nil, false, errors.New("the matrix must be a mapping")
	}
	if md == nil {
		return []MatrixCombo{{Values: expr.NewDictionary()}}, false, nil
	}
	var vecs []vector
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
			if !ok || arr == nil || arr.Len() == 0 {
				return nil, false, fmt.Errorf("matrix vector '%s' does not contain any values", p.Key)
			}
			if arr.Len() > MaxMatrixProduct {
				return nil, false, errProductTooBig
			}
			vecs = append(vecs, vector{key: p.Key, items: arr.Items()})
		}
	}
	return expand(ctx, vecs, includes, excludes)
}

func matrixEntries(name string, v expr.Value) ([]*expr.Dictionary, error) {
	arr, ok := v.(*expr.Array)
	if !ok || arr == nil {
		return nil, fmt.Errorf("matrix %s must be a sequence of mappings", name)
	}
	if arr.Len() > MaxMatrixEntries {
		return nil, fmt.Errorf("matrix %s has more than %d entries", name, MaxMatrixEntries)
	}
	var out []*expr.Dictionary
	for _, it := range arr.Items() {
		d, ok := it.(*expr.Dictionary)
		if !ok || d == nil {
			return nil, fmt.Errorf("matrix %s must be a sequence of mappings", name)
		}
		out = append(out, d)
	}
	return out, nil
}

// ErrExcludeUnknownKey is matched, with [errors.Is], by the error that
// [ExpandMatrix], [ParseStrategy] and [Job.EvaluateStrategy] return when an
// entry of "exclude" names a key that is not a vector of the matrix (keys
// that only "include" adds do not count). GitHub refuses such a workflow as
// invalid, before any job exists, and names the run after the file path
// rather than after the "name:" key (recorded: matrix-edge-values,
// a-exclude-unknown-key.yml). The error carries one line per offending
// entry; when several entries offend, errors.Is still holds.
var ErrExcludeUnknownKey = errors.New("matrix exclude key does not match any key within the matrix")

// excludeKeyError is the error for one exclude entry with an unknown key.
type excludeKeyError struct {
	// Key is the first unknown key of the entry.
	Key string
}

// Unwrap makes errors.Is(err, [ErrExcludeUnknownKey]) hold.
func (e *excludeKeyError) Unwrap() error { return ErrExcludeUnknownKey }

func (e *excludeKeyError) Error() string {
	return fmt.Sprintf("Matrix exclude key '%s' does not match any key within the matrix", e.Key)
}

// errProductTooBig is returned when the cross product of the vectors is too
// large to be enumerated. GitHub would count the configurations left after
// the exclusions; this limit keeps the work bounded, see [MaxMatrixProduct].
var errProductTooBig = fmt.Errorf("matrix too large: the cross product of the vectors exceeds %d combinations", MaxMatrixProduct)

// ErrTooManyConfigurations is matched, with [errors.Is], by the error that
// [ExpandMatrix], [ParseStrategy] and [Job.EvaluateStrategy] return when a
// matrix produces more than [MaxMatrixJobs] configurations. The error itself
// carries the job and the count in its text. It does not match the error for a
// cross product over [MaxMatrixProduct], which is a work bound of this package
// rather than a GitHub rule.
var ErrTooManyConfigurations = errors.New("matrix produces too many configurations")

// configurationLimitError is returned when a matrix produces more than
// [MaxMatrixJobs] configurations. Its text is the one observed on the
// github.com run page for matrix-over-256 (not in the recordings, which only
// hold the failed run): "Strategy for job 'too-many' produced 272
// configurations which exceeds the maximum of 256 configurations". Job is
// empty when the matrix was expanded without a job, see
// [Job.EvaluateStrategy].
type configurationLimitError struct {
	// Job is the id of the job.
	Job string
	// Count is the number of configurations the matrix produces.
	Count int
}

// Unwrap makes errors.Is(err, [ErrTooManyConfigurations]) hold.
func (e *configurationLimitError) Unwrap() error { return ErrTooManyConfigurations }

func (e *configurationLimitError) Error() string {
	subject := "Strategy"
	if e.Job != "" {
		subject = fmt.Sprintf("Strategy for job '%s'", e.Job)
	}
	return fmt.Sprintf("%s produced %d configurations which exceeds the maximum of %d configurations", subject, e.Count, MaxMatrixJobs)
}

type vector struct {
	key   string
	items []expr.Value
}

// condition says which items of vector key may appear in a combination that
// an exclude or include entry applies to.
type condition struct {
	vec int
	ok  []bool // by index of the item in the vector
}

type includeEntry struct {
	conds []condition
	extra []expr.Pair // pairs whose key is not a vector
}

func compileCondition(vecs []vector, vi int, want expr.Value) condition {
	c := condition{vec: vi, ok: make([]bool, len(vecs[vi].items))}
	for i, it := range vecs[vi].items {
		c.ok[i] = valuesMatch(it, want)
	}
	return c
}

func expand(ctx context.Context, vecs []vector, includes, excludes []*expr.Dictionary) ([]MatrixCombo, bool, error) {
	index := make(map[string]int, len(vecs))
	for i, v := range vecs {
		index[strings.ToLower(v.key)] = i
	}
	// Exclude entries become lists of conditions on item positions, so that
	// testing a combination costs a few array reads.
	var excl [][]condition
	var bad []error
	for _, ex := range excludes {
		var conds []condition
		unknown := false
		for _, p := range ex.Pairs() {
			vi, ok := index[strings.ToLower(p.Key)]
			if !ok {
				// One error per offending entry, for its first unknown key.
				bad = append(bad, &excludeKeyError{Key: p.Key})
				unknown = true
				break
			}
			conds = append(conds, compileCondition(vecs, vi, p.Value))
		}
		if len(conds) > 0 && !unknown {
			excl = append(excl, conds)
		}
	}
	if len(bad) > 0 {
		return nil, false, errors.Join(bad...)
	}
	incl := make([]includeEntry, len(includes))
	for n, inc := range includes {
		for _, p := range inc.Pairs() {
			if vi, ok := index[strings.ToLower(p.Key)]; ok {
				incl[n].conds = append(incl[n].conds, compileCondition(vecs, vi, p.Value))
			} else {
				incl[n].extra = append(incl[n].extra, p)
			}
		}
	}

	// total counts every configuration, so that the error of a matrix over the
	// limit tells how many it produces; only the first MaxMatrixJobs are built.
	var combos []MatrixCombo
	total := 0
	matched := make([]bool, len(includes))
	if len(vecs) > 0 {
		product := 1
		for _, v := range vecs {
			product *= len(v.items)
			if product > MaxMatrixProduct {
				return nil, false, errProductTooBig
			}
		}
		tuple := make([]int, len(vecs))
	next:
		for i := 0; i < product; i++ {
			if i%checkEvery == 0 {
				if err := ctx.Err(); err != nil {
					return nil, false, err
				}
			}
			if i > 0 {
				// Advance the last position first: the first key varies slowest.
				for k := len(tuple) - 1; k >= 0; k-- {
					tuple[k]++
					if tuple[k] < len(vecs[k].items) {
						break
					}
					tuple[k] = 0
				}
			}
			for _, conds := range excl {
				if allHold(conds, tuple) {
					continue next
				}
			}
			total++
			build := total <= MaxMatrixJobs
			var b *comboBuilder
			if build {
				b = &comboBuilder{vals: map[string]expr.Value{}}
				for k, v := range vecs {
					b.set(v.key, v.items[tuple[k]])
				}
			}
			for n, inc := range incl {
				if !allHold(inc.conds, tuple) {
					continue
				}
				matched[n] = true
				if build {
					for _, p := range inc.extra {
						b.set(p.Key, p.Value)
					}
				}
			}
			if build {
				combos = append(combos, b.combo())
			}
		}
	}
	for n, inc := range includes {
		if matched[n] {
			continue
		}
		total++
		if total > MaxMatrixJobs {
			continue
		}
		b := &comboBuilder{vals: map[string]expr.Value{}}
		for _, p := range inc.Pairs() {
			b.set(p.Key, p.Value)
		}
		combos = append(combos, b.combo())
	}
	if total > MaxMatrixJobs {
		return nil, false, &configurationLimitError{Count: total}
	}
	if len(combos) == 0 {
		if len(vecs) == 0 && len(includes) == 0 && len(excludes) == 0 {
			// An empty matrix runs the job once.
			return []MatrixCombo{{Values: expr.NewDictionary()}}, true, nil
		}
		// TODO(verify with recording): GitHub's behaviour when every
		// combination is excluded.
		return nil, false, errors.New("the matrix has no combinations: all of them are excluded")
	}
	return combos, true, nil
}

func allHold(conds []condition, tuple []int) bool {
	for _, c := range conds {
		if !c.ok[tuple[c.vec]] {
			return false
		}
	}
	return true
}

// comboBuilder holds the values of one combination while includes are
// applied; added values may be overwritten by later includes, original ones
// never are (an include's vector keys are conditions, not assignments).
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

// segment is the text a matrix value adds to the name of a job. Null and the
// empty string add nothing, numbers and booleans are shown as in an
// expression ("1.5", "true"); all recorded (matrix-edge-values:
// "default-name", "two-axes (x)"). Arrays and mappings add nothing: assumed,
// not recorded.
func segment(v expr.Value) (string, bool) {
	switch v.(type) {
	case nil, expr.Null, *expr.Array, *expr.Dictionary:
		return "", false
	}
	s := expr.CoerceString(v)
	return s, s != ""
}
