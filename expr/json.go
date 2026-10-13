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
	if p.i >= len(p.s) {
		return nil, p.fail("Error reading JToken from JsonReader.")
	}
	return p.value(0)
}

// jsonReaderMaxDepth is Json.NET's JsonReader.MaxDepth default.
const jsonReaderMaxDepth = 64

type jsonParser struct {
	s string
	i int
	// started is false until the first token is read (Json.NET's Start state):
	// an error at the first column of the text then reports line 0.
	started bool
	// stack is the open containers, for the path of an error.
	stack []jsonFrame
}

// jsonFrame is an open array or object, as far as the path of an error needs.
type jsonFrame struct {
	obj  bool
	n    int    // array: values read so far
	name string // object: the current property name
	has  bool
}

// JSONError is a failure of FromJSON. Its text is the message of the
// Newtonsoft.Json.JsonReaderException the runner raises for the same input,
// "<reason> Path '<path>', line <n>, position <n>.", which GitHub prints. The
// reasons, paths and positions are reproduced for the failures seen in
// practice (an unexpected character, a truncated text, a bad literal or number,
// an unterminated string...); for the rarer ones the reason or the position may
// differ from Json.NET's, but the text keeps the same form. See PORTING.md.
type JSONError struct {
	// Reason is the message without the position, e.g. "Unexpected character
	// encountered while parsing value: n."
	Reason string
	// Path is the JSON path of the last token read ("a.b[0]"), "" at the root.
	Path string
	// Line and Position are Json.NET's JsonTextReader.LineNumber and
	// LinePosition: 1-based lines (0 before the first token), 0-based
	// positions, counted in UTF-16 units.
	Line, Position int
}

func (e *JSONError) Error() string {
	return fmt.Sprintf("%s Path '%s', line %d, position %d.", e.Reason, e.Path, e.Line, e.Position)
}

// ExceptionClass is the .NET class of the exception, which GitHub prints in
// front of the message when it does not know where the expression is.
func (e *JSONError) ExceptionClass() string { return "Newtonsoft.Json.JsonReaderException" }

// fail builds the error for the current position.
func (p *jsonParser) fail(reason string) error {
	line, pos := 1, 0
	units := 0
	for i := 0; i < p.i && i < len(p.s); {
		r, w := utf8.DecodeRuneInString(p.s[i:])
		i += w
		switch {
		case r == '\n', r == '\r' && (i >= len(p.s) || p.s[i] != '\n'):
			line++
			units = 0
		case r == '\r':
			// the line break is counted with the following \n
			units++
		default:
			units++
			if r >= 0x10000 {
				units++
			}
		}
	}
	pos = units
	if !p.started && pos == 0 {
		line = 0
	}
	return &JSONError{Reason: reason, Path: p.path(), Line: line, Position: pos}
}

// path is JsonReader.Path: the property names and indexes of the open
// containers, the container being read only once it has a current element.
func (p *jsonParser) path() string {
	var sb strings.Builder
	for _, f := range p.stack {
		switch {
		case f.obj && f.has:
			if strings.ContainsAny(f.name, ".  '/\"[]()\t\n\r\f\b\\\u0085\u2028\u2029") {
				sb.WriteString("['" + strings.NewReplacer("\\", "\\\\", "'", "\\'").Replace(f.name) + "']")
			} else {
				if sb.Len() > 0 {
					sb.WriteByte('.')
				}
				sb.WriteString(f.name)
			}
		case !f.obj && f.n > 0:
			fmt.Fprintf(&sb, "[%d]", f.n-1)
		}
	}
	return sb.String()
}

func (p *jsonParser) top() *jsonFrame {
	if len(p.stack) == 0 {
		return nil
	}
	return &p.stack[len(p.stack)-1]
}

// valueRead counts a finished scalar in the open array.
func (p *jsonParser) valueRead() {
	p.started = true
	if f := p.top(); f != nil && !f.obj {
		f.n++
	}
}

// unexpected is the error for a character that cannot start a value (or, at
// the end of the text, for a text that stops too early).
func (p *jsonParser) unexpected() error {
	if p.i >= len(p.s) {
		return p.fail("Unexpected end when reading JSON.")
	}
	r, _ := utf8.DecodeRuneInString(p.s[p.i:])
	return p.fail("Unexpected character encountered while parsing value: " + string(r) + ".")
}

// unexpectedAfter is the error for a character where a separator or the end of
// the container should be.
func (p *jsonParser) unexpectedAfter() error {
	if p.i >= len(p.s) {
		return p.unterminated()
	}
	r, _ := utf8.DecodeRuneInString(p.s[p.i:])
	return p.fail("After parsing a value an unexpected character was encountered: " + string(r) + ".")
}

// unterminated is the error for a text that ends inside an array or object.
func (p *jsonParser) unterminated() error {
	f := p.top()
	if f == nil {
		return p.fail("Unexpected end when reading JSON.")
	}
	kind := "JArray"
	if f.obj {
		kind = "JObject"
	}
	if (f.obj && !f.has) || (!f.obj && f.n == 0) {
		return p.fail("Error reading " + kind + " from JsonReader.")
	}
	return p.fail("Unexpected end of content while loading " + kind + ".")
}

// badLiteral is the error for a word that starts like true, false, null, NaN,
// Infinity or undefined but is not one: Json.NET compares the characters one
// by one and fails at the first that differs.
func (p *jsonParser) badLiteral(word, kind string) error {
	if len(p.s)-p.i < len(word) {
		p.i = len(p.s)
		return p.fail("Unexpected end when reading JSON.")
	}
	k := 0
	for k < len(word) && p.s[p.i+k] == word[k] {
		k++
	}
	p.i += k
	return p.fail("Error parsing " + kind + " value.")
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
		p.valueRead()
		return String(s), nil
	case c == '-' || c == '.' || c >= '0' && c <= '9':
		v, err := p.number()
		if err == nil {
			p.valueRead()
		}
		return v, err
	case c == '/':
		p.i++
		if p.i >= len(p.s) {
			return nil, p.fail("Unexpected end while parsing comment.")
		}
		if p.s[p.i] != '*' {
			r, _ := utf8.DecodeRuneInString(p.s[p.i:])
			return nil, p.fail("Error parsing comment. Expected: *, got " + string(r) + ".")
		}
		p.i--
		return nil, p.fail("Comments are not supported by this port.")
	case c == ']' && len(p.stack) == 0:
		p.i++
		return nil, p.fail("JsonToken EndArray is not valid for closing JsonType None.")
	}
	if c := p.s[p.i]; c == ',' && depth == 0 {
		// A comma where the document should start is an undefined value,
		// which Json.NET reads as an empty string; whatever follows is ignored
		// like any text after the first value.
		return String(""), nil
	}
	start := p.i
	var v Value
	kind := ""
	switch word := p.token(); word {
	case "true":
		v, kind = Boolean(true), "boolean"
	case "false":
		v, kind = Boolean(false), "boolean"
	case "null":
		v, kind = Null{}, "null"
	case "undefined":
		v, kind = String(""), "undefined"
	case "NaN":
		v, kind = Number(nan), "NaN"
	case "Infinity":
		v, kind = Number(posInf), "Infinity"
	default:
		p.i = start
		return nil, p.literalError()
	}
	if err := p.afterLiteral(kind); err != nil {
		return nil, err
	}
	p.valueRead()
	return v, nil
}

// literalError explains a word that is not a literal, by its first letter.
func (p *jsonParser) literalError() error {
	next := byte(0)
	if p.i+1 < len(p.s) {
		next = p.s[p.i+1]
	}
	switch p.s[p.i] {
	case 't':
		return p.badLiteral("true", "boolean")
	case 'f':
		return p.badLiteral("false", "boolean")
	case 'n':
		if next == 'u' {
			return p.badLiteral("null", "null")
		}
	case 'N':
		return p.badLiteral("NaN", "NaN")
	case 'I':
		return p.badLiteral("Infinity", "Infinity")
	case 'u':
		return p.badLiteral("undefined", "undefined")
	}
	return p.unexpected()
}

// afterLiteral checks the character that follows a word literal (true, null,
// NaN...): Json.NET only accepts a "/" there when it starts a comment, and
// comments are not ported, so any "/" is an error here. Anything else is left
// for the caller (white space, a comma, a closing bracket, or trailing text).
func (p *jsonParser) afterLiteral(kind string) error {
	if p.i >= len(p.s) || p.s[p.i] != '/' {
		return nil
	}
	if p.i+1 < len(p.s) && (p.s[p.i+1] == '/' || p.s[p.i+1] == '*') {
		return p.fail("Comments are not supported by this port.")
	}
	return p.fail("Error parsing " + kind + " value.")
}

// number reads a number token: decimal with optional sign, fraction and
// exponent (a digit is required in the mantissa), -Infinity, or an unsigned
// 0x hexadecimal integer.
func (p *jsonParser) number() (Value, error) {
	start := p.i
	if strings.HasPrefix(p.s[p.i:], "-I") {
		if !strings.HasPrefix(p.s[p.i:], "-Infinity") {
			return nil, p.badLiteral("-Infinity", "-Infinity")
		}
	}
	tok, err := p.numberToken()
	if err != nil {
		return nil, err
	}
	if tok == "-Infinity" {
		if err := p.afterLiteral("-Infinity"); err != nil {
			return nil, err
		}
		return Number(math.Inf(-1)), nil
	}
	if len(tok) > 2 && tok[0] == '0' && (tok[1] == 'x' || tok[1] == 'X') {
		if len(tok) <= 18 {
			if n, err := strconv.ParseUint(tok[2:], 16, 64); err == nil && !strings.ContainsAny(tok[2:], "+-_") {
				return Number(float64(int64(n))), nil
			}
		}
		return nil, p.badNumber(start, tok)
	}
	// A non-negative number with a leading zero is octal when it has only
	// digits (010 is 8, 08 is an error) and an error unless the zero is
	// followed by ".", "e" or "E" (01.5, 012e1 and 00.5 fail). A negative
	// number is read as decimal whatever its zeros (-010 is -10).
	if len(tok) > 1 && tok[0] == '0' && tok[1] != '.' && tok[1] != 'e' && tok[1] != 'E' {
		v, ok := parseOctal64(tok[1:])
		if !ok {
			return nil, p.badNumber(start, tok)
		}
		return Number(v), nil
	}
	body := strings.TrimPrefix(tok, "-")
	if !isDecimalLiteral(body) {
		return nil, p.badNumber(start, tok)
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
	if f == 0 && isBigInteger(body) {
		f = 0 // an integer token is read as a long: "-0" and "-00" are plain 0
	}
	return Number(f), nil
}

// numberToken reads the characters of a number as JsonTextReader does: those
// of a fixed set (digits, signs, ".", a-f, A-F, x, X) up to a separator; any
// other character is an error at its own position.
func (p *jsonParser) numberToken() (string, error) {
	start := p.i
	for p.i < len(p.s) {
		c := p.s[p.i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F',
			c == '+', c == '-', c == '.', c == 'x', c == 'X', c == 'I', c == 'n', c == 'i', c == 't', c == 'y':
			if c == 'I' || c == 'n' || c == 'i' || c == 't' || c == 'y' {
				// only the letters of -Infinity are read on; anything else
				// is not in the set
				if !strings.HasPrefix(p.s[start:], "-Infinity") {
					return "", p.numberChar()
				}
			}
			p.i++
		default:
			r, _ := utf8.DecodeRuneInString(p.s[p.i:])
			if isJSONSeparator(r) {
				return p.s[start:p.i], nil
			}
			return "", p.numberChar()
		}
	}
	return p.s[start:p.i], nil
}

func (p *jsonParser) numberChar() error {
	r, _ := utf8.DecodeRuneInString(p.s[p.i:])
	return p.fail("Unexpected character encountered while parsing number: " + string(r) + ".")
}

// badNumber is the error for a malformed number token; Json.NET reports it at
// the end of the token, with the element already counted in the path.
func (p *jsonParser) badNumber(start int, tok string) error {
	p.i = start + len(tok)
	p.valueRead()
	return p.fail("Input string '" + tok + "' is not a valid number.")
}

// parseOctal64 reads octal digits as Convert.ToInt64(s, 8) does: leading
// zeros are free, at most 64 significant bits, and a value of 2^63 or more
// wraps to a negative number (1777777777777777777777 is -1).
func parseOctal64(digits string) (float64, bool) {
	if digits == "" {
		return 0, false
	}
	var v uint64
	for i := 0; i < len(digits); i++ {
		c := digits[i]
		if c < '0' || c > '7' {
			return 0, false
		}
		if v>>61 != 0 {
			return 0, false // the next shift would leave 64 bits
		}
		v = v<<3 | uint64(c-'0')
	}
	return float64(int64(v)), true
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
			return "", p.fail("Unterminated string. Expected delimiter: " + string(quote) + ".")
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
			return "", p.fail("Unterminated string. Expected delimiter: " + string(quote) + ".")
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
			return "", p.fail("Bad JSON escape sequence: \\" + string(e) + ".")
		}
	}
}

func (p *jsonParser) hex4() (rune, error) {
	// JsonTextReader needs a character after the four digits (EnsureChars).
	if p.i+4 >= len(p.s) {
		return 0, p.fail("Unexpected end while parsing Unicode escape sequence.")
	}
	first := p.i
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
			p.i = first
			return 0, p.fail("Invalid Unicode escape sequence: \\u" + p.s[first:first+4] + ".")
		}
		r = r<<4 | rune(d)
		p.i++
	}
	return r, nil
}

func (p *jsonParser) array(depth int) (Value, error) {
	if depth > jsonReaderMaxDepth {
		return nil, p.fail(fmt.Sprintf("The reader's MaxDepth of %d has been exceeded.", jsonReaderMaxDepth))
	}
	p.i++
	p.valueRead()
	p.stack = append(p.stack, jsonFrame{})
	arr := &Array{}
	for {
		p.skipSpace()
		if p.i >= len(p.s) {
			return nil, p.unterminated()
		}
		switch p.s[p.i] {
		case ']':
			p.i++
			p.pop()
			return arr, nil
		case ',':
			// An empty element is "undefined", which reads as an empty string.
			p.i++
			p.valueRead()
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
			return nil, p.unterminated()
		}
		switch p.s[p.i] {
		case ',':
			p.i++
		case ']':
		default:
			return nil, p.unexpectedAfter()
		}
	}
}

// pop closes the innermost container; it counts as a value of its parent.
func (p *jsonParser) pop() {
	p.stack = p.stack[:len(p.stack)-1]
}

// propertyName reads a quoted or unquoted object name.
func (p *jsonParser) propertyName() (string, error) {
	if p.s[p.i] == '"' || p.s[p.i] == '\'' {
		return p.str()
	}
	start := p.i
	if r, _ := utf8.DecodeRuneInString(p.s[p.i:]); !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '$' {
		return "", p.fail("Invalid property identifier character: " + string(r) + ".")
	}
	for p.i < len(p.s) {
		r, w := utf8.DecodeRuneInString(p.s[p.i:])
		if r == ':' || isNetSpace(r) {
			break
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '$' {
			return "", p.fail("Invalid JavaScript property identifier character: " + string(r) + ".")
		}
		p.i += w
	}
	if p.i >= len(p.s) {
		return "", p.fail("Unexpected end while parsing unquoted property name.")
	}
	if p.i == start {
		return "", p.unexpected()
	}
	return p.s[start:p.i], nil
}

func (p *jsonParser) object(depth int) (Value, error) {
	if depth > jsonReaderMaxDepth {
		return nil, p.fail(fmt.Sprintf("The reader's MaxDepth of %d has been exceeded.", jsonReaderMaxDepth))
	}
	p.i++
	p.valueRead()
	p.stack = append(p.stack, jsonFrame{obj: true})
	d := NewDictionary()
	for {
		p.skipSpace()
		if p.i >= len(p.s) {
			return nil, p.unterminated()
		}
		if p.s[p.i] == '}' {
			p.i++
			p.pop()
			return d, nil
		}
		k, err := p.propertyName()
		if err != nil {
			return nil, err
		}
		p.skipSpace()
		if p.i >= len(p.s) {
			return nil, p.unterminated()
		}
		if p.s[p.i] != ':' {
			r, _ := utf8.DecodeRuneInString(p.s[p.i:])
			return nil, p.fail("Invalid character after parsing property name. Expected ':' but got: " + string(r) + ".")
		}
		p.i++
		f := p.top()
		f.name, f.has = k, true
		p.skipSpace()
		if p.i >= len(p.s) {
			return nil, p.unterminated()
		}
		v, err := p.value(depth)
		if err != nil {
			return nil, err
		}
		d.set(k, v)
		p.skipSpace()
		if p.i >= len(p.s) {
			return nil, p.unterminated()
		}
		switch p.s[p.i] {
		case ',':
			p.i++
		case '}':
		default:
			return nil, p.unexpectedAfter()
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
