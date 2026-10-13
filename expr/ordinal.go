// SPDX-License-Identifier: MIT

package expr

import (
	"sort"
	"strings"
)

// The runner compares and looks up strings with .NET's
// StringComparison.OrdinalIgnoreCase and StringComparer.OrdinalIgnoreCase:
// every code point is replaced by its simple upper-case form (a per-character
// table, not full Unicode case mapping), then the UTF-16 code units are
// compared. Sources, all in actions/runner src/Sdk/DTExpressions2/Expressions2:
//
//	== != < <= > >=   EvaluationResult.cs AbstractEqual/AbstractGreaterThan/
//	                  AbstractLessThan: String.Equals / String.Compare
//	contains          Sdk/Functions/Contains.cs: IndexOf(..., OrdinalIgnoreCase)
//	startsWith        Sdk/Functions/StartsWith.cs
//	endsWith          Sdk/Functions/EndsWith.cs
//	contexts, functions and dictionary keys:
//	                  ExpressionParser.cs ParseContext and
//	                  DTPipelines/Pipelines/ContextData/DictionaryContextData.cs
//	                  (Dictionary<string,int>(StringComparer.OrdinalIgnoreCase))
//
// Differences from the JavaScript reference: "ß" is not "SS" (no multi-
// character mappings), the Kelvin sign is not "k", the dotless "ı" and the
// long "ſ" stay themselves, and contains() uses the same folding as the other
// comparisons. fold_tables.go is generated from the runner's own behaviour
// (go generate, gen/runner --fold).

//go:generate sh -c "dotnet gen/runner/bin/Release/net9.0/runner.dll --fold | go run gen/fold.go -o fold_tables.go"

// ordinalFoldRune returns the code point that r is folded to.
func ordinalFoldRune(r rune) rune {
	if r < 0x80 {
		if r >= 'a' && r <= 'z' {
			return r - 'a' + 'A'
		}
		return r
	}
	i := sort.Search(len(ordinalUpper), func(i int) bool { return ordinalUpper[i][0] >= r })
	if i < len(ordinalUpper) && ordinalUpper[i][0] == r {
		return ordinalUpper[i][1]
	}
	return r
}

// ordinalFold folds s. Bytes that are not UTF-8 become U+FFFD, as everywhere
// in this package.
func ordinalFold(s string) string {
	if isASCII(s) {
		return strings.ToUpper(s)
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, w := decodeRune(s[i:])
		i += w
		writeRune(&b, ordinalFoldRune(r))
	}
	return b.String()
}

// OrdinalFold returns s folded the way .NET's OrdinalIgnoreCase compares
// strings (the runner's HashSet and Dictionary keys): every code point is
// replaced by its simple upper-case form from a table generated from the
// runner's own behaviour. Non-ASCII characters never fold into ASCII ones
// ("ſ" and the Kelvin sign stay themselves), unlike JavaScript's toUpperCase.
// Two strings are equal under OrdinalIgnoreCase iff their folds are equal.
func OrdinalFold(s string) string { return ordinalFold(s) }

// ordinalEqual is String.Equals(a, b, OrdinalIgnoreCase).
func ordinalEqual(a, b string) bool { return ordinalFold(a) == ordinalFold(b) }

// ordinalCompare is Math.Sign(String.Compare(a, b, OrdinalIgnoreCase)). The
// runner orders by code point after folding (a supplementary character sorts
// after U+E000..U+FFFF, unlike in JavaScript, which compares UTF-16 units),
// which is the byte order of UTF-8.
func ordinalCompare(a, b string) int { return strings.Compare(ordinalFold(a), ordinalFold(b)) }

// foldKey is the case-insensitive form of a dictionary key, context name or
// function name.
func foldKey(key string) string { return ordinalFold(key) }
