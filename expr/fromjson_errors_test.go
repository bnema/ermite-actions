// SPDX-License-Identifier: MIT

package expr

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

// unportedMessage reports whether the runner's message is about something this
// package does not read: comments, constructors ("new Date(1)"), and the
// stray-bracket check of JContainer.
func unportedMessage(in, msg string) bool {
	return strings.Contains(in, "/") || strings.Contains(in, "new ") ||
		strings.HasPrefix(msg, "Unexpected content while parsing JSON") || strings.Contains(msg, "constructor") ||
		strings.HasPrefix(msg, "JsonToken ") && !strings.Contains(msg, "closing JsonType None")
}

// TestFromJSONErrorsMatchRunner replays testdata/fromjson_errors.json, the
// messages of the runner's JsonReaderException for the inputs fromJSON rejects
// (gen/runner/fromjson_errors.mjs). GitHub prints them in the job log.
func TestFromJSONErrorsMatchRunner(t *testing.T) {
	data, err := os.ReadFile("testdata/fromjson_errors.json")
	if err != nil {
		t.Fatal(err)
	}
	var cs []struct{ In, Msg string }
	if err := json.Unmarshal(data, &cs); err != nil {
		t.Fatal(err)
	}
	bad, skipped := 0, 0
	for _, c := range cs {
		_, err := FromJSON([]byte(c.In))
		if err != nil && err.Error() == c.Msg {
			continue
		}
		if unportedMessage(c.In, c.Msg) {
			skipped++
			continue
		}
		bad++
		if bad <= 25 {
			t.Errorf("FromJSON(%q): %v\n  runner: %s", c.In, err, c.Msg)
		}
	}
	t.Logf("%d cases, %d unported (comments, constructors), %d mismatches", len(cs), skipped, bad)
	if skipped > len(cs)/10 {
		t.Errorf("too many cases skipped: %d", skipped)
	}
}

// The forms of the conformance corpus (expr-runtime-errors): fromJSON of
// "not json", the same text the runner fails on.
func TestFromJSONErrorText(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"not json", "Unexpected character encountered while parsing value: n. Path '', line 0, position 0."},
		{"", "Error reading JToken from JsonReader. Path '', line 0, position 0."},
		{"  \n ", "Error reading JToken from JsonReader. Path '', line 2, position 1."},
		{"[1,x]", "Unexpected character encountered while parsing value: x. Path '[0]', line 1, position 3."},
		{`{"a":{"b":x}}`, "Unexpected character encountered while parsing value: x. Path 'a.b', line 1, position 10."},
		{"{", "Error reading JObject from JsonReader. Path '', line 1, position 1."},
		{"[1,2", "Unexpected end of content while loading JArray. Path '[1]', line 1, position 4."},
		{`"abc`, "Unterminated string. Expected delimiter: \". Path '', line 1, position 4."},
		{"tru", "Unexpected end when reading JSON. Path '', line 1, position 3."},
		{"[nul]", "Error parsing null value. Path '', line 1, position 4."},
		{"08", "Input string '08' is not a valid number. Path '', line 1, position 2."},
		{"[1\n,\n x]", "Unexpected character encountered while parsing value: x. Path '[0]', line 3, position 1."},
	} {
		_, err := FromJSON([]byte(tc.in))
		if err == nil || err.Error() != tc.want {
			t.Errorf("FromJSON(%q) = %v, want %s", tc.in, err, tc.want)
		}
		var je *JSONError
		if !errors.As(err, &je) || je.ExceptionClass() != "Newtonsoft.Json.JsonReaderException" {
			t.Errorf("FromJSON(%q): not a *JSONError: %T", tc.in, err)
		}
	}
}

// fromJSON() fails with the reader's message, and the error unwraps to it.
func TestFromJSONFunctionError(t *testing.T) {
	node, err := Parse("fromJSON(env.x)", []string{"env"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Evaluate(context.Background(), node, EvalContext{Contexts: map[string]Value{"env": func() Value {
		d := NewDictionary()
		d.Add("x", String("not json"))
		return d
	}()}})
	want := "Unexpected character encountered while parsing value: n. Path '', line 0, position 0."
	var ee *EvalError
	var je *JSONError
	if !errors.As(err, &ee) || ee.Message != want || !errors.As(err, &je) {
		t.Fatalf("%v", err)
	}
}
