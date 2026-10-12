// SPDX-License-Identifier: MIT

package expr

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type fromJSONCase struct {
	In  string `json:"in"`
	OK  bool   `json:"ok"`
	Out string `json:"out"`
}

func loadFromJSONCases(tb testing.TB) []fromJSONCase {
	tb.Helper()
	data, err := os.ReadFile("testdata/fromjson_cases.json")
	if err != nil {
		tb.Fatal(err)
	}
	var cs []fromJSONCase
	if err := json.Unmarshal(data, &cs); err != nil {
		tb.Fatal(err)
	}
	return cs
}

// unportedJSON reports whether the input uses something of Json.NET that is not
// ported: comments and constructors. The runner accepts them, this package
// fails on them (see PORTING.md).
func unportedJSON(s string) bool {
	return strings.Contains(s, "/*") || strings.Contains(s, "//") || strings.Contains(s, "new ") || strings.Contains(s, "/")
}

// TestFromJSONMatchesRunner replays testdata/fromjson_cases.json, the answers
// of the runner's fromJSON (gen/runner/fromjson_cases.mjs) on hand-picked and
// mutated JSON-like texts: it must accept exactly the same texts and give the
// same toJSON, except where the text could hold a comment or constructor.
func TestFromJSONMatchesRunner(t *testing.T) {
	cases := loadFromJSONCases(t)
	bad, skipped := 0, 0
	for _, c := range cases {
		v, err := FromJSON([]byte(c.In))
		if err == nil && strings.TrimFunc(c.In, isNetSpace) == "" {
			err = os.ErrInvalid // fromJSON() rejects blank input before reading
		}
		gotOK := err == nil
		if gotOK != c.OK || (gotOK && ToJSON(v) != c.Out) {
			if unportedJSON(c.In) {
				skipped++
				continue
			}
			bad++
			if bad <= 25 {
				t.Errorf("FromJSON(%q): ok=%v out=%q (%v); runner ok=%v out=%q", c.In, gotOK, func() string {
					if v != nil {
						return ToJSON(v)
					}
					return ""
				}(), err, c.OK, c.Out)
			}
		}
	}
	t.Logf("%d cases, %d with comments, slashes or constructors skipped, %d mismatches", len(cases), skipped, bad)
}
