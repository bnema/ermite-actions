// SPDX-License-Identifier: MIT

package expr

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"unicode"
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
// The reader is Json.NET's, as in the runner (Sdk/Functions/FromJson.cs:
// JToken.ReadFrom on a JsonTextReader, then ToPipelineContextData), so it
// accepts more than RFC 8259:
//
//   - only the first value is read, anything after it is ignored;
//   - strings may use single quotes and may contain raw control characters,
//     object names may be unquoted (letters, digits, _ and $);
//   - a trailing comma is accepted, an empty array element is an empty string;
//   - NaN, Infinity and -Infinity are numbers, numbers may be written .5, 5.,
//     1.e2, 01 or 0x1F (hexadecimal, no sign), and undefined is an empty
//     string;
//   - nesting is limited to 64 levels (JsonReader.MaxDepth).
//
// Not ported (they fail here): comments and constructors (new Date(1)).
// Numbers are float64 (1e999 is +Inf). Objects become [*Dictionary] values in
// document order; a repeated key, compared case-insensitively, keeps the
// spelling and position of its first occurrence and the value of its last.
// An unpaired surrogate escape reads as U+FFFD.
func FromJSON(data []byte) (Value, error) {
	s := string(data)
	if !utf8.ValidString(s) {
		s = string([]rune(s)) // invalid bytes become U+FFFD
	}
	p := &jsonParser{s: s}
	p.skipSpace()
	return p.value(0)
}

// jsonReaderMaxDepth is Json.NET's JsonReader.MaxDepth default.
const jsonReaderMaxDepth = 64

type jsonParser struct {
	s string
	i int
}

func (p *jsonParser) unexpected() error {
	if p.i >= len(p.s) {
		return errors.New("unexpected end of JSON input")
	}
	r, _ := utf8.DecodeRuneInString(p.s[p.i:])
	return fmt.Errorf("unexpected character %q in JSON at position %d", r, p.i)
}

func (p *jsonParser) skipSpace() {
	for p.i < len(p.s) {
		r, w := utf8.DecodeRuneInString(p.s[p.i:])
		if !isNetSpace(r) {
			return
		}
		p.i += w
	}
}

// isJSONSeparator reports whether c ends a literal or a number.
func isJSONSeparator(r rune) bool {
	switch r {
	case ',', '}', ']', ')', '/':
		return true
	}
	return isNetSpace(r)
}

// token returns the run of characters up to the next separator.
func (p *jsonParser) token() string {
	start := p.i
	for p.i < len(p.s) {
		r, w := utf8.DecodeRuneInString(p.s[p.i:])
		if isJSONSeparator(r) {
			break
		}
		p.i += w
	}
	return p.s[start:p.i]
}

func (p *jsonParser) value(depth int) (Value, error) {
	if p.i >= len(p.s) {
		return nil, p.unexpected()
	}
	switch c := p.s[p.i]; {
	case c == '{':
		return p.object(depth + 1)
	case c == '[':
		return p.array(depth + 1)
	case c == '"' || c == '\'':
		s, err := p.str()
		if err != nil {
			return nil, err
		}
		return String(s), nil
	case c == '-' || c == '.' || c >= '0' && c <= '9':
		return p.number()
	}
	start := p.i
	switch word := p.token(); word {
	case "true":
		return Boolean(true), nil
	case "false":
		return Boolean(false), nil
	case "null":
		return Null{}, nil
	case "undefined":
		return String(""), nil
	case "NaN":
		return Number(nan), nil
	case "Infinity":
		return Number(posInf), nil
	}
	p.i = start
	return nil, p.unexpected()
}

// number reads a number token: decimal with optional sign, fraction and
// exponent (a digit is required in the mantissa), -Infinity, or an unsigned
// 0x hexadecimal integer.
func (p *jsonParser) number() (Value, error) {
	start := p.i
	tok := p.token()
	if tok == "-Infinity" {
		return Number(math.Inf(-1)), nil
	}
	if len(tok) > 2 && tok[0] == '0' && (tok[1] == 'x' || tok[1] == 'X') {
		if len(tok) <= 18 {
			if n, err := strconv.ParseUint(tok[2:], 16, 64); err == nil && !strings.ContainsAny(tok[2:], "+-_") {
				return Number(float64(int64(n))), nil
			}
		}
		p.i = start
		return nil, fmt.Errorf("input string %q is not a valid number", tok)
	}
	body := strings.TrimPrefix(tok, "-")
	if !isDecimalLiteral(body) {
		p.i = start
		return nil, fmt.Errorf("input string %q is not a valid number", tok)
	}
	if isBigInteger(body) {
		// Json.NET reads an integer that does not fit in 64 bits as a
		// BigInteger, and (Double)BigInteger truncates toward zero to 53 bits
		// instead of rounding to nearest.
		if _, err := strconv.ParseInt(tok, 10, 64); err != nil {
			n, _ := new(big.Int).SetString(tok, 10)
			f, _ := new(big.Float).SetPrec(53).SetMode(big.ToZero).SetInt(n).Float64()
			return Number(f), nil
		}
	}
	f, _ := strconv.ParseFloat(tok, 64) // range errors give ±Inf
	return Number(f), nil
}

// isBigInteger reports whether s is only digits.
func isBigInteger(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}

// str reads a string literal; p.i is on the opening quote.
func (p *jsonParser) str() (string, error) {
	quote := p.s[p.i]
	p.i++
	var b strings.Builder
	for {
		if p.i >= len(p.s) {
			return "", errors.New("unterminated string in JSON")
		}
		c := p.s[p.i]
		if c == quote {
			p.i++
			return b.String(), nil
		}
		if c != '\\' {
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
		case '"', '\'', '\\', '/':
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
			if r >= 0xD800 && r < 0xE000 {
				r = utf8.RuneError // an unpaired surrogate
			}
			b.WriteRune(r)
		default:
			p.i -= 2
			return "", fmt.Errorf("bad JSON escape sequence \\%c at position %d", e, p.i)
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
	if depth > jsonReaderMaxDepth {
		return nil, fmt.Errorf("the reader's MaxDepth of %d has been exceeded", jsonReaderMaxDepth)
	}
	p.i++
	arr := &Array{}
	for {
		p.skipSpace()
		if p.i >= len(p.s) {
			return nil, p.unexpected()
		}
		switch p.s[p.i] {
		case ']':
			p.i++
			return arr, nil
		case ',':
			// An empty element is "undefined", which reads as an empty string.
			p.i++
			arr.items = append(arr.items, String(""))
			continue
		}
		v, err := p.value(depth)
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
		default:
			return nil, p.unexpected()
		}
	}
}

// propertyName reads a quoted or unquoted object name.
func (p *jsonParser) propertyName() (string, error) {
	if p.s[p.i] == '"' || p.s[p.i] == '\'' {
		return p.str()
	}
	start := p.i
	for p.i < len(p.s) {
		r, w := utf8.DecodeRuneInString(p.s[p.i:])
		if r == ':' || isNetSpace(r) {
			break
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '$' {
			return "", p.unexpected()
		}
		p.i += w
	}
	if p.i == start {
		return "", p.unexpected()
	}
	return p.s[start:p.i], nil
}

func (p *jsonParser) object(depth int) (Value, error) {
	if depth > jsonReaderMaxDepth {
		return nil, fmt.Errorf("the reader's MaxDepth of %d has been exceeded", jsonReaderMaxDepth)
	}
	p.i++
	d := NewDictionary()
	for {
		p.skipSpace()
		if p.i >= len(p.s) {
			return nil, p.unexpected()
		}
		if p.s[p.i] == '}' {
			p.i++
			return d, nil
		}
		k, err := p.propertyName()
		if err != nil {
			return nil, err
		}
		p.skipSpace()
		if p.i >= len(p.s) || p.s[p.i] != ':' {
			return nil, p.unexpected()
		}
		p.i++
		p.skipSpace()
		v, err := p.value(depth)
		if err != nil {
			return nil, err
		}
		d.set(k, v)
		p.skipSpace()
		if p.i >= len(p.s) {
			return nil, p.unexpected()
		}
		switch p.s[p.i] {
		case ',':
			p.i++
		case '}':
		default:
			return nil, p.unexpected()
		}
	}
}

// ToJSON renders v as the toJSON function does (Sdk/Functions/ToJson.cs): two
// space indentation, "key": value pairs in the dictionary's order, "[]" and
// "{}" for empty containers. A number is written as its string form
// (numberToCoercedString: at most 15 significant digits, so NaN and the
// infinities are written as such). Strings are escaped as Json.NET does by
// default: quotes, backslashes, \b \f \n \r \t, the other control characters
// as \u00xx, and U+0085, U+2028 and U+2029 as \uxxxx; everything else,
// including non-ASCII text, is written as is.
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
		// The runner writes the number as it converts it to a string
		// (ToJson.WriteValue: value.ConvertToString()), so NaN and the
		// infinities are written as such, and a number has at most 15
		// significant digits. JSON.stringify gives null and the shortest
		// round-trip form.
		w.str(numberToCoercedString(float64(x)))
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
		keys := x.keys
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
		case r == 0x85 || r == 0x2028 || r == 0x2029 || r >= 0xD800 && r < 0xE000:
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
