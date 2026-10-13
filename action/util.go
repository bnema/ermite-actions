// SPDX-License-Identifier: MIT

package action

import (
	"fmt"
	"strings"
	"unicode"

	"bnema.dev/ermite-actions/template"
)

// asciiEqualFold is String.Equals(a, b, OrdinalIgnoreCase) for the strings the
// manifest compares: .NET's simple case mapping folds no non-ASCII character
// into ASCII, so only A-Z and a-z fold.
func asciiEqualFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		x, y := a[i], b[i]
		if x == y {
			continue
		}
		if x >= 'a' && x <= 'z' {
			x -= 'a' - 'A'
		}
		if y >= 'a' && y <= 'z' {
			y -= 'a' - 'A'
		}
		if x != y {
			return false
		}
	}
	return true
}

// utf16Len is String.Length.
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// isNullOrWhiteSpace is String.IsNullOrWhiteSpace.
func isNullOrWhiteSpace(s string) bool { return strings.TrimFunc(s, unicode.IsSpace) == "" }

// The .NET exception classes the runner's loader raises.
const (
	exArgument     = "System.ArgumentException"
	exArgumentNull = "System.ArgumentNullException"
	exArgumentRng  = "System.ArgumentOutOfRangeException"
	exNotSupported = "System.NotSupportedException"
	exNullRef      = "System.NullReferenceException"
	exInvalidOp    = "System.InvalidOperationException"
	exTemplate     = "GitHub.DistributedTask.ObjectTemplating.TemplateValidationException"
)

// thrown stands for a .NET exception: the loader panics with it where the
// runner throws, and Parse records it where the runner catches it.
type thrown struct{ class, msg string }

func throwf(class, format string, a ...any) { panic(thrown{class, fmt.Sprintf(format, a...)}) }

// typeName is the CLR type name of a token, "" for null, as in the messages
// of the runner's Assert methods.
func typeName(t template.Token) string {
	if t == nil {
		return ""
	}
	return t.Kind().String()
}

func assertFail(t template.Token, desc, want string) {
	throwf(exArgument, "Unexpected type '%s' encountered while reading '%s'. The type '%s' was expected.", typeName(t), desc, want)
}

func assertString(t template.Token, desc string) *template.String {
	if s, ok := t.(*template.String); ok {
		return s
	}
	assertFail(t, desc, "StringToken")
	return nil
}

func assertMapping(t template.Token, desc string) *template.Mapping {
	if m, ok := t.(*template.Mapping); ok {
		return m
	}
	assertFail(t, desc, "MappingToken")
	return nil
}

func assertSequence(t template.Token, desc string) *template.Sequence {
	if s, ok := t.(*template.Sequence); ok {
		return s
	}
	assertFail(t, desc, "SequenceToken")
	return nil
}

func assertBoolean(t template.Token, desc string) *template.Boolean {
	if b, ok := t.(*template.Boolean); ok {
		return b
	}
	assertFail(t, desc, "BooleanToken")
	return nil
}

func assertScalar(t template.Token, desc string) template.Token {
	if t != nil && template.IsScalar(t) {
		return t
	}
	assertFail(t, desc, "ScalarToken")
	return nil
}

// trimSpaceJS is String.prototype.trim.
func trimSpaceJS(s string) string {
	return strings.TrimFunc(s, func(r rune) bool { return unicode.IsSpace(r) || r == '\ufeff' })
}
