// SPDX-License-Identifier: MIT

package action

import (
	"fmt"
	"unicode/utf16"
)

// refNames is a port of the runner's ReferenceNameBuilder
// (Sdk/DTPipelines/Pipelines/ObjectTemplating/ReferenceNameBuilder.cs), which
// validates the ids written in a composite action and makes the ones the file
// leaves out. It is not package workflow's idBuilder: the runner's accepts ids
// that start with "__", lets an id of 100 characters or more through, counts
// UTF-16 code units, and words its error without the reference parser's
// doubled "and".
type refNames struct {
	name     []uint16
	distinct map[string]bool // upper-cased with ASCII rules
}

const maxNodeNameLength = 100

func newRefNames() *refNames { return &refNames{distinct: map[string]bool{}} }

func asciiUpper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 'a' + 'A'
		}
	}
	return string(b)
}

// add records name and reports whether it was new (HashSet.Add with
// OrdinalIgnoreCase; a name that is not ASCII is one that validName refused,
// or one of 100 characters or more, which are compared by their ASCII upper
// case here).
func (b *refNames) add(name string) bool {
	k := asciiUpper(name)
	if b.distinct[k] {
		return false
	}
	b.distinct[k] = true
	return true
}

// appendSegment is AppendSegment.
func (b *refNames) appendSegment(value string) {
	if value == "" {
		return
	}
	units := utf16.Encode([]rune(value))
	if len(b.name) == 0 {
		first := units[0]
		legal := first >= 'a' && first <= 'z' || first >= 'A' && first <= 'Z' || first == '_'
		if !legal && (first >= '0' && first <= '9' || first == '-') {
			b.name = append(b.name, '_')
		}
	} else {
		b.name = append(b.name, '_')
	}
	for _, c := range units {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-':
			b.name = append(b.name, c)
		default:
			b.name = append(b.name, '_')
		}
	}
}

// build is Build: the name made from the segments ("job" when there are none)
// made unique with a numeric suffix.
func (b *refNames) build() string {
	original := "job"
	if len(b.name) > 0 {
		original = string(utf16.Decode(b.name))
	}
	for attempt := 1; ; attempt++ {
		suffix := ""
		if attempt > 1 {
			if attempt >= 1000 {
				throwf(exInvalidOp, "Unable to create a unique name")
			}
			suffix = fmt.Sprintf("_%d", attempt)
		}
		// original holds only ASCII: appendSegment replaced everything else.
		candidate := original[:min(len(original), maxNodeNameLength-len(suffix))] + suffix
		if b.add(candidate) {
			b.name = b.name[:0]
			return candidate
		}
	}
}

// tryAddKnownName is TryAddKnownName: it returns the error text, or "". Note
// the runner's condition: an invalid name is only refused when it is shorter
// than 100 characters.
func (b *refNames) tryAddKnownName(value string) string {
	if !validName(value) && utf16Len(value) < maxNodeNameLength {
		return fmt.Sprintf("The identifier '%s' is invalid. IDs may only contain alphanumeric characters, '_', and '-'. IDs must start with a letter or '_' and must be less than %d characters.", value, maxNodeNameLength)
	}
	if !b.add(value) {
		return fmt.Sprintf("The identifier '%s' may not be used more than once within the same scope.", value)
	}
	return ""
}

// validName is NameValidation.IsValid(name, allowHyphens: true).
func validName(name string) bool {
	for i, c := range utf16.Encode([]rune(name)) {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_':
		case i > 0 && (c >= '0' && c <= '9' || c == '-'):
		default:
			return false
		}
	}
	return true
}
