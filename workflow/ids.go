// SPDX-License-Identifier: MIT

package workflow

import (
	"fmt"
	"strings"
)

const (
	idSeparator   = "_"
	idMaxAttempts = 1000
	idMaxLength   = 100
)

// idBuilder is a port of IdBuilder in the reference: it validates the ids
// written in a file and makes unique ids for steps that have none.
type idBuilder struct {
	name     []rune
	distinct map[string]bool
}

func newIDBuilder() *idBuilder { return &idBuilder{distinct: map[string]bool{}} }

func isAlpha(c rune) bool   { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func isNumeric(c rune) bool { return c >= '0' && c <= '9' }

func (b *idBuilder) appendSegment(value string) {
	if value == "" {
		return
	}
	rs := []rune(value)
	if len(b.name) == 0 {
		first := rs[0]
		switch {
		case isAlpha(first) || first == '_':
		case isNumeric(first) || first == '-':
			b.name = append(b.name, '_')
		}
	} else {
		b.name = append(b.name, '_')
	}
	for _, c := range rs {
		if isAlpha(c) || isNumeric(c) || c == '_' || c == '-' {
			b.name = append(b.name, c)
		} else {
			b.name = append(b.name, '_')
		}
	}
}

// build returns a unique id from the appended segments, "job" when there are
// none. It returns "" when a thousand ids are taken.
func (b *idBuilder) build() string {
	original := "job"
	if len(b.name) > 0 {
		original = string(b.name)
	}
	for attempt := 1; attempt < idMaxAttempts; attempt++ {
		suffix := ""
		if attempt > 1 {
			suffix = fmt.Sprintf("_%d", attempt)
		}
		n := min(len(original), idMaxLength-len(suffix))
		candidate := original[:n] + suffix
		if !b.distinct[candidate] {
			b.distinct[candidate] = true
			b.name = nil
			return candidate
		}
	}
	return ""
}

// tryAddKnownID records an id written in the file. It returns the error
// message when the id is invalid or already used.
func (b *idBuilder) tryAddKnownID(value string) string {
	if value == "" || !validID(value) || len(value) >= idMaxLength {
		return fmt.Sprintf("The identifier '%s' is invalid. IDs may only contain alphanumeric characters, '_', and '-'. IDs must start with a letter or '_' and and must be less than %d characters.", value, idMaxLength)
	}
	if strings.HasPrefix(value, idSeparator+idSeparator) {
		return fmt.Sprintf("The identifier '%s' is invalid. IDs starting with '__' are reserved.", value)
	}
	if b.distinct[value] {
		return fmt.Sprintf("The identifier '%s' may not be used more than once within the same scope.", value)
	}
	b.distinct[value] = true
	return ""
}

func validID(name string) bool {
	for i, c := range name {
		if i == 0 {
			if !isAlpha(c) && c != '_' {
				return false
			}
			continue
		}
		if !isAlpha(c) && !isNumeric(c) && c != '_' && c != '-' {
			return false
		}
	}
	return true
}
