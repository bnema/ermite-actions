// SPDX-License-Identifier: MIT

package expr

import (
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

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

func jsUpperRune(r rune) string {
	if s, ok := jsUpperDelta[r]; ok {
		return s
	}
	return string(unicode.ToUpper(r))
}

// jsToUpper is String.prototype.toUpperCase.
func jsToUpper(s string) string {
	if isASCII(s) {
		return strings.ToUpper(s)
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		b.WriteString(jsUpperRune(r))
	}
	return b.String()
}

// toUpperSpecial upper-cases s like jsToUpper but leaves the dotless i
// (U+0131) alone, as the reference implementation does for its
// case-insensitive comparisons and for startsWith and endsWith.
func toUpperSpecial(s string) string {
	if isASCII(s) {
		return strings.ToUpper(s)
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == 'ı' {
			b.WriteRune(r)
			continue
		}
		b.WriteString(jsUpperRune(r))
	}
	return b.String()
}

// jsToLower is String.prototype.toLowerCase, including the Final_Sigma
// context rule (a capital sigma at the end of a word becomes ς).
func jsToLower(s string) string {
	if isASCII(s) {
		return strings.ToLower(s)
	}
	runes := []rune(s)
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
				b.WriteRune(unicode.ToLower(r))
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

// compareUTF16 compares two strings by UTF-16 code unit, which is how
// JavaScript orders strings with < and >. It differs from byte or rune order
// only for supplementary-plane characters against U+E000..U+FFFF.
func compareUTF16(a, b string) int {
	for len(a) > 0 && len(b) > 0 {
		ra, wa := utf8.DecodeRuneInString(a)
		rb, wb := utf8.DecodeRuneInString(b)
		if ra != rb {
			if utf16Key(ra) < utf16Key(rb) {
				return -1
			}
			return 1
		}
		a, b = a[wa:], b[wb:]
	}
	switch {
	case len(a) > 0:
		return 1
	case len(b) > 0:
		return -1
	}
	return 0
}

// utf16Key packs the UTF-16 encoding of r into an integer that sorts like
// the code unit sequence.
func utf16Key(r rune) uint32 {
	hi, lo := utf16.EncodeRune(r)
	if hi == unicode.ReplacementChar && lo == unicode.ReplacementChar {
		return uint32(r) << 16 // BMP (EncodeRune reports no pair)
	}
	return uint32(hi)<<16 | uint32(lo)
}
