// SPDX-License-Identifier: MIT

package expr

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// MaxJSONDepth bounds the nesting that FromJSON accepts and that ToJSON and
// EncodeJSON write. JavaScript engines fail with a stack overflow somewhere
// in that region; fromJSON reports it like any other parse error. The bound
// also stops the encoder on cyclic values, which can be built through
// [Array.Append].
const MaxJSONDepth = 1000

// FromJSON parses JSON text into a [Value], like the fromJSON function.
//
// The grammar is strict RFC 8259 JSON as JSON.parse accepts it. Numbers are
// float64 (1e999 is +Inf). Objects become [*Dictionary] values: keys that
// are array indices ("0", "42") come first in ascending order, then the rest
// in document order, as JavaScript enumerates object keys; a repeated key
// keeps its first position and its last value; keys that differ only by case
// collapse to the first.
func FromJSON(data []byte) (Value, error) {
	s := string(data)
	if !utf8.ValidString(s) {
		s = string([]rune(s)) // invalid bytes become U+FFFD, as in a JS string
	}
	p := &jsonParser{s: s}
	p.skipSpace()
	v, err := p.value(0)
	if err != nil {
		return nil, err
	}
	p.skipSpace()
	if p.i < len(p.s) {
		return nil, p.unexpected()
	}
	return v, nil
}

type jsonParser struct {
	s string
	i int
}

func (p *jsonParser) unexpected() error {
	if p.i >= len(p.s) {
		return errors.New("unexpected end of JSON input")
	}
	r, _ := utf8.DecodeRuneInString(p.s[p.i:])
	return fmt.Errorf("unexpected token %q in JSON at position %d", r, p.i)
}

func (p *jsonParser) skipSpace() {
	for p.i < len(p.s) {
		switch p.s[p.i] {
		case ' ', '\t', '\n', '\r':
			p.i++
		default:
			return
		}
	}
}

func (p *jsonParser) literal(word string, v Value) (Value, error) {
	if !strings.HasPrefix(p.s[p.i:], word) {
		// Advance to the first mismatching byte for the error position.
		for j := 0; j < len(word) && p.i < len(p.s) && p.s[p.i] == word[j]; j++ {
			p.i++
		}
		return nil, p.unexpected()
	}
	p.i += len(word)
	return v, nil
}

func (p *jsonParser) value(depth int) (Value, error) {
	if depth > MaxJSONDepth {
		return nil, errors.New("maximum JSON nesting depth exceeded")
	}
	if p.i >= len(p.s) {
		return nil, p.unexpected()
	}
	switch c := p.s[p.i]; {
	case c == '{':
		return p.object(depth)
	case c == '[':
		return p.array(depth)
	case c == '"':
		s, err := p.str()
		if err != nil {
			return nil, err
		}
		return String(s), nil
	case c == 't':
		return p.literal("true", Boolean(true))
	case c == 'f':
		return p.literal("false", Boolean(false))
	case c == 'n':
		return p.literal("null", Null{})
	case c == '-' || c >= '0' && c <= '9':
		return p.number()
	}
	return nil, p.unexpected()
}

func (p *jsonParser) number() (Value, error) {
	start := p.i
	if p.s[p.i] == '-' {
		p.i++
	}
	digits := func() int {
		n := 0
		for p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
			p.i++
			n++
		}
		return n
	}
	switch {
	case p.i < len(p.s) && p.s[p.i] == '0':
		p.i++
	case digits() == 0:
		return nil, p.unexpected()
	}
	if p.i < len(p.s) && p.s[p.i] == '.' {
		p.i++
		if digits() == 0 {
			return nil, p.unexpected()
		}
	}
	if p.i < len(p.s) && (p.s[p.i] == 'e' || p.s[p.i] == 'E') {
		p.i++
		if p.i < len(p.s) && (p.s[p.i] == '+' || p.s[p.i] == '-') {
			p.i++
		}
		if digits() == 0 {
			return nil, p.unexpected()
		}
	}
	f, _ := strconv.ParseFloat(p.s[start:p.i], 64) // range errors give ±Inf
	return Number(f), nil
}

// str reads a string literal; p.i is on the opening quote.
func (p *jsonParser) str() (string, error) {
	p.i++
	var b strings.Builder
	for {
		if p.i >= len(p.s) {
			return "", errors.New("unterminated string in JSON")
		}
		c := p.s[p.i]
		switch {
		case c == '"':
			p.i++
			return b.String(), nil
		case c < 0x20:
			return "", p.unexpected()
		case c != '\\':
			b.WriteByte(c)
			p.i++
			continue
		}
		p.i++
		if p.i >= len(p.s) {
			return "", errors.New("unterminated string in JSON")
		}
		e := p.s[p.i]
		p.i++
		switch e {
		case '"', '\\', '/':
			b.WriteByte(e)
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'u':
			r, err := p.hex4()
			if err != nil {
				return "", err
			}
			if r >= 0xD800 && r < 0xDC00 && strings.HasPrefix(p.s[p.i:], `\u`) {
				save := p.i
				p.i += 2
				lo, err := p.hex4()
				if err != nil {
					return "", err
				}
				if lo >= 0xDC00 && lo < 0xE000 {
					r = 0x10000 + (r-0xD800)<<10 + (lo - 0xDC00)
				} else {
					p.i = save // not a pair; the second escape is read on its own
				}
			}
			// A lone surrogate cannot be a Go rune: writeRune keeps it in
			// its three-byte WTF-8 form so that toJSON writes it back as
			// the same \uXXXX escape, like JSON.stringify.
			writeRune(&b, r)
		default:
			p.i--
			return "", p.unexpected()
		}
	}
}

func (p *jsonParser) hex4() (rune, error) {
	if p.i+4 > len(p.s) {
		p.i = len(p.s)
		return 0, p.unexpected()
	}
	var r rune
	for k := 0; k < 4; k++ {
		c := p.s[p.i]
		var d byte
		switch {
		case c >= '0' && c <= '9':
			d = c - '0'
		case c >= 'a' && c <= 'f':
			d = c - 'a' + 10
		case c >= 'A' && c <= 'F':
			d = c - 'A' + 10
		default:
			return 0, p.unexpected()
		}
		r = r<<4 | rune(d)
		p.i++
	}
	return r, nil
}

func (p *jsonParser) array(depth int) (Value, error) {
	p.i++
	arr := &Array{}
	p.skipSpace()
	if p.i < len(p.s) && p.s[p.i] == ']' {
		p.i++
		return arr, nil
	}
	for {
		p.skipSpace()
		v, err := p.value(depth + 1)
		if err != nil {
			return nil, err
		}
		arr.items = append(arr.items, v)
		p.skipSpace()
		if p.i >= len(p.s) {
			return nil, p.unexpected()
		}
		switch p.s[p.i] {
		case ',':
			p.i++
		case ']':
			p.i++
			return arr, nil
		default:
			return nil, p.unexpected()
		}
	}
}

func (p *jsonParser) object(depth int) (Value, error) {
	p.i++
	var keys []string
	vals := map[string]Value{}
	p.skipSpace()
	if p.i < len(p.s) && p.s[p.i] == '}' {
		p.i++
		return NewDictionary(), nil
	}
	for {
		p.skipSpace()
		if p.i >= len(p.s) || p.s[p.i] != '"' {
			return nil, p.unexpected()
		}
		k, err := p.str()
		if err != nil {
			return nil, err
		}
		p.skipSpace()
		if p.i >= len(p.s) || p.s[p.i] != ':' {
			return nil, p.unexpected()
		}
		p.i++
		p.skipSpace()
		v, err := p.value(depth + 1)
		if err != nil {
			return nil, err
		}
		if _, dup := vals[k]; !dup {
			keys = append(keys, k)
		}
		vals[k] = v // last value wins, at the first position
		p.skipSpace()
		if p.i >= len(p.s) {
			return nil, p.unexpected()
		}
		switch p.s[p.i] {
		case ',':
			p.i++
			continue
		case '}':
			p.i++
		default:
			return nil, p.unexpected()
		}
		break
	}
	d := NewDictionary()
	for _, k := range jsKeyOrder(keys) {
		d.Add(k, vals[k])
	}
	return d, nil
}

// isArrayIndex reports whether k is the canonical decimal form of an integer
// in [0, 2^32-2], the keys JavaScript enumerates before all others.
func isArrayIndex(k string) bool {
	if k == "" || len(k) > 10 || (len(k) > 1 && k[0] == '0') {
		return false
	}
	for i := 0; i < len(k); i++ {
		if k[i] < '0' || k[i] > '9' {
			return false
		}
	}
	n, _ := strconv.ParseUint(k, 10, 64)
	return n < math.MaxUint32
}

// jsKeyOrder reorders keys the way Object.keys enumerates a JavaScript
// object: array indices ascending, then the others in their given order.
func jsKeyOrder(keys []string) []string {
	var idx, rest []string
	for _, k := range keys {
		if isArrayIndex(k) {
			idx = append(idx, k)
		} else {
			rest = append(rest, k)
		}
	}
	if len(idx) == 0 {
		return keys
	}
	sort.Slice(idx, func(i, j int) bool {
		a, _ := strconv.ParseUint(idx[i], 10, 64)
		b, _ := strconv.ParseUint(idx[j], 10, 64)
		return a < b
	})
	return append(idx, rest...)
}

// ToJSON renders v as the toJSON function does: JSON.stringify(v, replacer,
// "  "), that is two-space indentation, "key": value pairs, "[]" and "{}"
// for empty containers, and numbers as JavaScript prints them (NaN and the
// infinities as null, -0 as 0, 1e21 as 1e+21). Dictionary keys that are array
// indices come first in ascending order, as when GitHub's replacer builds a
// JavaScript object. Control characters, quotes and backslashes in strings
// are escaped; everything else, including non-ASCII text, is written as is.
// Lone surrogates, which FromJSON keeps in WTF-8 form, are written as \uXXXX.
//
// ToJSON suits the finite values that FromJSON and Evaluate produce. A value
// nested deeper than [MaxJSONDepth], which only a cyclic value built by hand
// can be, makes it return "null"; use [EncodeJSON] to get an error instead
// and to bound the output.
func ToJSON(v Value) string {
	s, err := EncodeJSON(v, -1)
	if err != nil {
		return "null"
	}
	return s
}

// EncodeJSON is [ToJSON] with limits: it fails when v is nested deeper than
// [MaxJSONDepth] and, when maxBytes is not negative, as soon as the output
// would exceed maxBytes bytes, without building the rest.
func EncodeJSON(v Value, maxBytes int) (string, error) {
	w := &jsonWriter{limit: maxBytes}
	w.value(v, 0)
	if w.err != nil {
		return "", w.err
	}
	return w.b.String(), nil
}

var (
	errJSONTooDeep = errors.New("value nested too deeply to encode as JSON (a cycle?)")
	errJSONTooBig  = errors.New("JSON output exceeds the size limit")
)

type jsonWriter struct {
	b     strings.Builder
	limit int // negative: unlimited
	err   error
}

// grow reports whether n more bytes may be written, recording the error
// otherwise.
func (w *jsonWriter) grow(n int) bool {
	if w.err != nil {
		return false
	}
	if w.limit >= 0 && n > w.limit-w.b.Len() {
		w.err = errJSONTooBig
		return false
	}
	return true
}

func (w *jsonWriter) str(s string) {
	if w.grow(len(s)) {
		w.b.WriteString(s)
	}
}

func (w *jsonWriter) indent(depth int) {
	if w.grow(2 * depth) {
		for i := 0; i < depth; i++ {
			w.b.WriteString("  ")
		}
	}
}

func (w *jsonWriter) value(v Value, depth int) {
	if w.err != nil {
		return
	}
	if depth > MaxJSONDepth {
		w.err = errJSONTooDeep
		return
	}
	switch x := normalize(v).(type) {
	case Null:
		w.str("null")
	case Boolean:
		if x {
			w.str("true")
		} else {
			w.str("false")
		}
	case Number:
		f := float64(x)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			w.str("null")
		} else {
			w.str(jsNumberToString(f))
		}
	case String:
		w.jsonString(string(x))
	case *Array:
		if len(x.items) == 0 {
			w.str("[]")
			return
		}
		w.str("[\n")
		for i, it := range x.items {
			w.indent(depth + 1)
			w.value(it, depth+1)
			if i < len(x.items)-1 {
				w.str(",")
			}
			w.str("\n")
			if w.err != nil {
				return
			}
		}
		w.indent(depth)
		w.str("]")
	case *Dictionary:
		if len(x.keys) == 0 {
			w.str("{}")
			return
		}
		keys := jsKeyOrder(x.keys)
		w.str("{\n")
		for i, k := range keys {
			val, _ := x.Get(k)
			w.indent(depth + 1)
			w.jsonString(k)
			w.str(": ")
			w.value(val, depth+1)
			if i < len(keys)-1 {
				w.str(",")
			}
			w.str("\n")
			if w.err != nil {
				return
			}
		}
		w.indent(depth)
		w.str("}")
	}
}

const hexDigits = "0123456789abcdef"

func (w *jsonWriter) jsonString(s string) {
	if !w.grow(len(s) + 2) {
		return
	}
	w.b.WriteByte('"')
	for i := 0; i < len(s); {
		r, n := decodeRune(s[i:])
		i += n
		var esc string
		switch {
		case r == '"':
			esc = `\"`
		case r == '\\':
			esc = `\\`
		case r == '\b':
			esc = `\b`
		case r == '\f':
			esc = `\f`
		case r == '\n':
			esc = `\n`
		case r == '\r':
			esc = `\r`
		case r == '\t':
			esc = `\t`
		case r < 0x20:
			esc = `\u00` + string([]byte{hexDigits[r>>4], hexDigits[r&0xF]})
		case r >= 0xD800 && r < 0xE000:
			// A lone surrogate (WTF-8, see FromJSON).
			esc = `\u` + string([]byte{hexDigits[r>>12], hexDigits[r>>8&0xF], hexDigits[r>>4&0xF], hexDigits[r&0xF]})
		}
		if esc != "" {
			if !w.grow(len(esc)) {
				return
			}
			w.b.WriteString(esc)
			continue
		}
		if !w.grow(utf8.RuneLen(r)) {
			return
		}
		w.b.WriteRune(r) // other invalid UTF-8 comes out as U+FFFD
	}
	if w.grow(1) {
		w.b.WriteByte('"')
	}
}
