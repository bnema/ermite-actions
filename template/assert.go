// SPDX-License-Identifier: MIT

package template

import "fmt"

// TokenError is an error about a token, positioned at it. It is what the
// Assert functions return.
type TokenError struct {
	Token   Token
	Message string
}

func (e *TokenError) Error() string { return e.Message }

func assertErr(t Token, desc string, want string) error {
	return &TokenError{Token: t, Message: fmt.Sprintf("Unexpected type '%s' encountered while reading '%s'. The type '%s' was expected.", t.Kind(), desc, want)}
}

// AssertString returns t as a string token or a [*TokenError] worded like the
// reference's "Unexpected type 'X' encountered while reading 'desc'".
func AssertString(t Token, desc string) (*String, error) {
	if s, ok := t.(*String); ok {
		return s, nil
	}
	return nil, assertErr(t, desc, KindString.String())
}

// AssertBoolean returns t as a boolean token or a [*TokenError].
func AssertBoolean(t Token, desc string) (*Boolean, error) {
	if s, ok := t.(*Boolean); ok {
		return s, nil
	}
	return nil, assertErr(t, desc, KindBoolean.String())
}

// AssertNumber returns t as a number token or a [*TokenError].
func AssertNumber(t Token, desc string) (*Number, error) {
	if s, ok := t.(*Number); ok {
		return s, nil
	}
	return nil, assertErr(t, desc, KindNumber.String())
}

// AssertSequence returns t as a sequence token or a [*TokenError].
func AssertSequence(t Token, desc string) (*Sequence, error) {
	if s, ok := t.(*Sequence); ok {
		return s, nil
	}
	return nil, assertErr(t, desc, KindSequence.String())
}

// AssertMapping returns t as a mapping token or a [*TokenError].
func AssertMapping(t Token, desc string) (*Mapping, error) {
	if s, ok := t.(*Mapping); ok {
		return s, nil
	}
	return nil, assertErr(t, desc, KindMapping.String())
}

// AssertScalar returns t when it is neither a sequence nor a mapping, or a
// [*TokenError].
func AssertScalar(t Token, desc string) (Token, error) {
	if IsScalar(t) {
		return t, nil
	}
	return nil, assertErr(t, desc, "ScalarToken")
}
