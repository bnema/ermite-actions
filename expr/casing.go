// SPDX-License-Identifier: MIT

package expr

import (
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

// The tables in casing_tables.go are generated; see go:generate in doc.go.

// This file reproduces the JavaScript string functions the reference
// implementation relies on: toUpperCase and toLowerCase (full Unicode case
// mapping, including multi-character results such as "ß" to "SS" and the
// Final_Sigma rule), trim, and the \s character class.

// isJSSpace reports whether r is in JavaScript's WhiteSpace or
// LineTerminator sets. It is both the \s regular expression class and the
// set removed by String.prototype.trim and by Number().
func isJSSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xA0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

// jsTrim is String.prototype.trim.
func jsTrim(s string) string {
	return strings.TrimFunc(s, isJSSpace)
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// Strings are Go strings, so text that JavaScript would hold as a lone
// UTF-16 surrogate (from fromJSON('"\ud800"')) is kept in WTF-8: the
// three-byte form 0xED 0xA0-0xBF 0x80-0xBF that UTF-8 forbids. The helpers
// below make every case and comparison function treat such a sequence as one
// code unit and copy it unchanged, as JavaScript does, instead of letting Go
// turn each byte into U+FFFD.

// wtf8Surrogate reports whether s starts with a WTF-8 lone surrogate and
// returns its code unit.
func wtf8Surrogate(s string) (rune, bool) {
	if len(s) >= 3 && s[0] == 0xED && s[1] >= 0xA0 && s[1] <= 0xBF && s[2]&0xC0 == 0x80 {
		return rune(s[0]&0x0F)<<12 | rune(s[1]&0x3F)<<6 | rune(s[2]&0x3F), true
	}
	return 0, false
}

// decodeRune is utf8.DecodeRuneInString that also decodes a WTF-8 lone
// surrogate (width 3) to its code unit value.
func decodeRune(s string) (rune, int) {
	r, w := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError && w == 1 {
		if sur, ok := wtf8Surrogate(s); ok {
			return sur, 3
		}
	}
	return r, w
}

// writeRune writes r, using the WTF-8 form for a surrogate code unit.
func writeRune(b *strings.Builder, r rune) {
	if r >= 0xD800 && r < 0xE000 {
		b.WriteByte(0xE0 | byte(r>>12))
		b.WriteByte(0x80 | byte(r>>6)&0x3F)
		b.WriteByte(0x80 | byte(r)&0x3F)
		return
	}
	b.WriteRune(r)
}

// normalizeWTF8 joins a lone high surrogate immediately followed by a lone low
// surrogate into the supplementary character they form, as concatenating two
// JavaScript strings does. Strings built by concatenation (format, join) go
// through it.
func normalizeWTF8(s string) string {
	if !strings.Contains(s, "\xed") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, w := decodeRune(s[i:])
		if r >= 0xD800 && r < 0xDC00 {
			if lo, lw := decodeRune(s[i+w:]); lo >= 0xDC00 && lo < 0xE000 && lw == 3 && i+w < len(s) {
				b.WriteRune(0x10000 + (r-0xD800)<<10 + (lo - 0xDC00))
				i += w + lw
				continue
			}
		}
		b.WriteString(s[i : i+w])
		i += w
	}
	return b.String()
}

func writeUpper(b *strings.Builder, r rune, keepDotlessI bool) {
	if keepDotlessI && r == 'ı' {
		b.WriteRune(r)
		return
	}
	if s, ok := jsUpperDelta[r]; ok {
		b.WriteString(s)
		return
	}
	writeRune(b, unicode.ToUpper(r))
}

func upperWith(s string, keepDotlessI bool) string {
	if isASCII(s) {
		return strings.ToUpper(s)
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, w := decodeRune(s[i:])
		i += w
		writeUpper(&b, r, keepDotlessI)
	}
	return b.String()
}

// jsToUpper is String.prototype.toUpperCase.
func jsToUpper(s string) string { return upperWith(s, false) }

// toUpperSpecial upper-cases s like jsToUpper but leaves the dotless i
// (U+0131) alone, as the reference implementation does for its
// case-insensitive comparisons and for startsWith and endsWith.
func toUpperSpecial(s string) string { return upperWith(s, true) }

// jsToLower is String.prototype.toLowerCase, including the Final_Sigma
// context rule (a capital sigma at the end of a word becomes ς).
func jsToLower(s string) string {
	if isASCII(s) {
		return strings.ToLower(s)
	}
	runes := make([]rune, 0, len(s))
	for i := 0; i < len(s); {
		r, w := decodeRune(s[i:])
		runes = append(runes, r)
		i += w
	}
	var b strings.Builder
	b.Grow(len(s))
	for i, r := range runes {
		switch r {
		case 0x3A3:
			if finalSigma(runes, i) {
				b.WriteRune('ς')
			} else {
				b.WriteRune('σ')
			}
		default:
			if m, ok := jsLowerDelta[r]; ok {
				b.WriteString(m)
			} else {
				writeRune(&b, unicode.ToLower(r))
			}
		}
	}
	return b.String()
}

// finalSigma implements the Final_Sigma condition of Unicode SpecialCasing:
// the sigma is preceded by a cased letter, ignoring case-ignorable
// characters, and is not followed by one.
func finalSigma(runes []rune, i int) bool {
	before := false
	for j := i - 1; j >= 0; j-- {
		if isCaseIgnorable(runes[j]) {
			continue
		}
		before = isCased(runes[j])
		break
	}
	if !before {
		return false
	}
	for j := i + 1; j < len(runes); j++ {
		if isCaseIgnorable(runes[j]) {
			continue
		}
		return !isCased(runes[j])
	}
	return true
}

func isCased(r rune) bool {
	return unicode.IsUpper(r) || unicode.IsLower(r) || unicode.IsTitle(r) ||
		unicode.Is(unicode.Other_Lowercase, r) || unicode.Is(unicode.Other_Uppercase, r)
}

func isCaseIgnorable(r rune) bool {
	switch r {
	case '\'', '.', ':', '^', '`', 0xA8, 0xAD, 0xAF, 0xB4, 0xB7, 0xB8, 0x2018, 0x2019, 0x2024, 0x2027, 0xFE13, 0xFE52, 0xFE55, 0xFF07, 0xFF0E, 0xFF1A:
		return true
	}
	return unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf, unicode.Lm, unicode.Sk)
}

// utf16Units encodes s as the UTF-16 code units a JavaScript string holds,
// WTF-8 lone surrogates included.
func utf16Units(s string) []uint16 {
	out := make([]uint16, 0, len(s))
	for i := 0; i < len(s); {
		r, w := decodeRune(s[i:])
		i += w
		if r >= 0xD800 && r < 0xE000 {
			out = append(out, uint16(r))
			continue
		}
		if r >= 0x10000 {
			hi, lo := utf16.EncodeRune(r)
			out = append(out, uint16(hi), uint16(lo))
			continue
		}
		out = append(out, uint16(r))
	}
	return out
}

// compareUTF16 compares two strings by UTF-16 code unit, which is how
// JavaScript orders strings with < and >. It differs from byte or rune order
// only for supplementary-plane characters against U+E000..U+FFFF.
func compareUTF16(a, b string) int {
	if isASCII(a) && isASCII(b) {
		return strings.Compare(a, b)
	}
	ua, ub := utf16Units(a), utf16Units(b)
	for i := 0; i < len(ua) && i < len(ub); i++ {
		if ua[i] != ub[i] {
			if ua[i] < ub[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(ua) > len(ub):
		return 1
	case len(ua) < len(ub):
		return -1
	}
	return 0
}

// exceedsUTF16Len reports whether s is longer than max UTF-16 code units. It
// stops counting at max and allocates nothing. Invalid UTF-8 bytes count as
// one unit each (U+FFFD), like the conversion Lex applies.
func exceedsUTF16Len(s string, max int) bool {
	n := 0
	for _, r := range s {
		n++
		if r >= 0x10000 {
			n++
		}
		if n > max {
			return true
		}
	}
	return false
}

// hasWTF8 reports whether s holds a lone surrogate in WTF-8 form.
func hasWTF8(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == 0xED {
			if _, ok := wtf8Surrogate(s[i:]); ok {
				return true
			}
		}
	}
	return false
}

// jsIncludes, jsHasPrefix and jsHasSuffix are String.prototype.includes,
// startsWith and endsWith. They work on bytes, which is equivalent to UTF-16
// code units except around lone surrogates (a lone high surrogate is found
// inside a supplementary character), where they switch to code units.
func jsIncludes(s, sub string) bool {
	if !hasWTF8(s) && !hasWTF8(sub) {
		return strings.Contains(s, sub)
	}
	us, usub := utf16Units(s), utf16Units(sub)
	for i := 0; i+len(usub) <= len(us); i++ {
		if unitsEqual(us[i:i+len(usub)], usub) {
			return true
		}
	}
	return false
}

func jsHasPrefix(s, prefix string) bool {
	if !hasWTF8(s) && !hasWTF8(prefix) {
		return strings.HasPrefix(s, prefix)
	}
	us, up := utf16Units(s), utf16Units(prefix)
	return len(up) <= len(us) && unitsEqual(us[:len(up)], up)
}

func jsHasSuffix(s, suffix string) bool {
	if !hasWTF8(s) && !hasWTF8(suffix) {
		return strings.HasSuffix(s, suffix)
	}
	us, up := utf16Units(s), utf16Units(suffix)
	return len(up) <= len(us) && unitsEqual(us[len(us)-len(up):], up)
}

func unitsEqual(a, b []uint16) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return len(a) == len(b)
}
