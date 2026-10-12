// SPDX-License-Identifier: MIT

package expr

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
)

type evalCase struct {
	expr string
	want string // "Kind:string", "PARSE message" or "ERR message"
}

func runEvalCases(t *testing.T, cases []evalCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.expr, func(t *testing.T) {
			if got := evalString(t, c.expr); got != c.want {
				t.Errorf("%s\n got: %q\nwant: %q", c.expr, got, c.want)
			}
		})
	}
}

// Examples from the GitHub documentation of expressions.
func TestEvalDocumentationExamples(t *testing.T) {
	runEvalCases(t, []evalCase{
		{"contains('Hello world', 'llo')", "Boolean:true"},
		{"contains('Hello world', 'LLO')", "Boolean:true"},
		{"contains(github.event.issue.labels.*.name, 'bug')", "Boolean:true"},
		{"contains(github.event.issue.labels.*.name, 'BUG')", "Boolean:true"},
		{"contains(github.event.issue.labels.*.name, 'nope')", "Boolean:false"},
		{"startsWith('Hello world', 'He')", "Boolean:true"},
		{"startsWith('Hello world', 'he')", "Boolean:true"},
		{"startsWith('Hello world', 'world')", "Boolean:false"},
		{"endsWith('Hello world', 'ld')", "Boolean:true"},
		{"endsWith('Hello world', 'Hello')", "Boolean:false"},
		{"format('Hello {0} {1} {2}', 'Mona', 'the', 'Octocat')", "String:Hello Mona the Octocat"},
		{"format('{{Hello {0} {1} {2}!}}', 'Mona', 'the', 'Octocat')", "String:{Hello Mona the Octocat!}"},
		{"join(github.event.issue.labels.*.name, ', ')", "String:bug, Docs"},
		{"join(github.event.issue.labels.*.name)", "String:bug,Docs"},
		{"toJSON(fromJSON('{\"a\":1}'))", "String:{\n  \"a\": 1\n}"},
		{"fromJSON('{\"a\":[1,2]}').a[1]", "Number:2"},
		{"fromJSON('true')", "Boolean:true"},
		{"fromJSON('\"str\"')", "String:str"},
		{"fromJSON('null')", "Null:"},
		// The documented idiom for matrix from JSON and for converting a
		// string to a number: fromJSON('6') is a number.
		{"fromJSON('6') == 6", "Boolean:true"},
		{"github.event_name == 'push' && contains(github.ref, 'main')", "Boolean:true"},
		{"github.event_name == 'push' || false", "Boolean:true"},
		{"!(github.event_name == 'push')", "Boolean:false"},
		// Documented ternary idiom: cond && a || b.
		{"github.event_name == 'push' && 'yes' || 'no'", "String:yes"},
		{"github.event_name == 'pull_request' && 'yes' || 'no'", "String:no"},
		// A missing property is null, so the default after || applies.
		{"github.event.nope || 'default'", "String:default"},
		{"env.MISSING || 'fallback'", "String:fallback"},
		{"github.event.commits.*.id", "Array:Array"},
		{"join(github.event.commits.*.id, '-')", "String:1-2-3"},
	})
}

func TestEvalLiteralsAndOperators(t *testing.T) {
	runEvalCases(t, []evalCase{
		{"true", "Boolean:true"},
		{"false", "Boolean:false"},
		{"null", "Null:"},
		{"1", "Number:1"},
		{"-1", "Number:-1"},
		{"+1", "Number:1"},
		{"1.5", "Number:1.5"},
		{".5", "Number:0.5"},
		{"-.5", "Number:-0.5"},
		{"5.", "Number:5"},
		{"1e3", "Number:1000"},
		{"0x10", "Number:16"},
		{"0o17", "Number:15"},
		{"0b101", "Number:5"},
		{"-0x10", "PARSE Unexpected symbol: '-0x10'. Located at position 1 within expression: -0x10"},
		{"NaN", "Number:NaN"},
		{"Infinity", "Number:Infinity"},
		{"-Infinity", "Number:-Infinity"},
		{"'a'", "String:a"},
		{"''", "String:"},
		{"'it''s'", "String:it's"},
		// Every doubled quote is unescaped, as the C# runner does (the
		// JavaScript reference stops after the first).
		{"'a''b''c'", "String:a'b'c"},
		{"''''", "String:'"},
		{"'a''''b'", "String:a''b"},

		{"1 == 1", "Boolean:true"},
		{"1 == 2", "Boolean:false"},
		{"1 != 2", "Boolean:true"},
		{"1 < 2", "Boolean:true"},
		{"2 <= 2", "Boolean:true"},
		{"2 > 3", "Boolean:false"},
		{"3 >= 3", "Boolean:true"},

		// Loose equality: strings compare to numbers as numbers, null and
		// booleans become numbers.
		{"1 == '1'", "Boolean:true"},
		{"'1' == 1", "Boolean:true"},
		{"'1.0' == 1", "Boolean:true"},
		{"' 1 ' == 1", "Boolean:true"},
		{"'0x10' == 16", "Boolean:true"},
		{"'abc' == 0", "Boolean:false"},
		{"'' == 0", "Boolean:true"},
		{"'' == null", "Boolean:true"}, // both become 0
		{"null == 0", "Boolean:true"},
		{"null == false", "Boolean:true"},
		{"null == ''", "Boolean:true"},
		{"null == null", "Boolean:true"},
		{"true == 1", "Boolean:true"},
		{"true == '1'", "Boolean:true"},
		{"true == 'true'", "Boolean:false"},
		{"false == 0", "Boolean:true"},
		{"false == ''", "Boolean:true"},
		{"true == true", "Boolean:true"},
		{"true != false", "Boolean:true"},
		{"matrix.nothing == null", "Boolean:true"},
		{"env.EMPTY == null", "Boolean:true"},
		{"env.EMPTY == false", "Boolean:true"},

		// NaN is never equal, never ordered.
		{"NaN == NaN", "Boolean:false"},
		{"NaN != NaN", "Boolean:true"},
		{"NaN < 1", "Boolean:false"},
		{"NaN > 1", "Boolean:false"},
		{"NaN <= NaN", "Boolean:false"},
		{"NaN >= NaN", "Boolean:false"},
		{"'abc' > 1", "Boolean:false"},
		{"'abc' < 1", "Boolean:false"},
		{"'abc' != 1", "Boolean:true"},
		{"Infinity == Infinity", "Boolean:true"},
		{"-Infinity < Infinity", "Boolean:true"},

		// Strings compare case-insensitively and by upper-cased value.
		{"'a' == 'A'", "Boolean:true"},
		{"'abc' == 'ABC'", "Boolean:true"},
		{"'a' < 'B'", "Boolean:true"},
		{"'B' > 'a'", "Boolean:true"},
		{"'a' < 'A'", "Boolean:false"},
		{"'a' <= 'A'", "Boolean:true"},
		{"'Z' < 'a'", "Boolean:false"},
		{"'_' < 'a'", "Boolean:false"}, // '_' (0x5F) sorts after upper-case letters
		// OrdinalIgnoreCase folds one code point at a time: no ß to SS.
		{"'ß' == 'SS'", "Boolean:false"},
		{"'ß' == 'ss'", "Boolean:false"},
		{"'ß' == 'ß'", "Boolean:true"},
		{"'é' == 'É'", "Boolean:true"},
		{"'ǆ' == 'ǅ'", "Boolean:true"},
		{"'ı' == 'I'", "Boolean:false"},
		{"'\u212a' == 'k'", "Boolean:false"}, // Kelvin sign
		{"'ſ' == 's'", "Boolean:false"},
		{"'ς' == 'σ'", "Boolean:true"},  // both fold to Σ
		{"'ı' == 'I'", "Boolean:false"}, // dotless i is not upper-cased
		{"'ı' == 'i'", "Boolean:false"},
		{"'ı' == 'ı'", "Boolean:true"},
		{"'İ' == 'i'", "Boolean:false"},
		{"'σ' == 'ς'", "Boolean:true"},
		{"'ǆ' == 'Ǆ'", "Boolean:true"},

		// Booleans order false < true; null does not order (it becomes 0
		// against a number but is only equal to itself).
		{"false < true", "Boolean:true"},
		{"true > false", "Boolean:true"},
		{"true < false", "Boolean:false"},
		{"true >= true", "Boolean:true"},
		{"null < 1", "Boolean:true"},
		{"null < null", "Boolean:false"},
		{"null <= null", "Boolean:true"},
		{"null >= null", "Boolean:true"},
		{"true > 0", "Boolean:true"},

		// Arrays and dictionaries are equal only by identity and never ordered.
		{"matrix.node == matrix.node", "Boolean:true"},
		{"matrix.node != matrix.node", "Boolean:false"},
		{"matrix.node == fromJSON('[14,16,18]')", "Boolean:false"},
		{"matrix.obj == matrix.obj", "Boolean:true"},
		{"matrix.obj == fromJSON('{\"k\":\"v\"}')", "Boolean:false"},
		{"matrix.node == 1", "Boolean:false"},
		{"matrix.node < matrix.node", "Boolean:false"},
		{"matrix.node <= matrix.node", "Boolean:true"},
		{"matrix.node == null", "Boolean:false"},
		{"matrix.obj == true", "Boolean:false"},
		{"matrix.obj == 'Object'", "Boolean:false"},
		// A filtered array is a new array each time.
		{"github.event.commits.* == github.event.commits.*", "Boolean:false"},
	})
}

func TestEvalLogicalReturnsOperands(t *testing.T) {
	runEvalCases(t, []evalCase{
		{"1 || 2", "Number:1"},
		{"0 || 2", "Number:2"},
		{"0 || ''", "String:"},
		{"0 || '' || null", "Null:"},
		{"0 || '' || 'x'", "String:x"},
		{"null || null", "Null:"},
		{"1 && 2", "Number:2"},
		{"0 && 2", "Number:0"},
		{"1 && 0 && 3", "Number:0"},
		{"1 && 2 && 3", "Number:3"},
		{"'a' && 'b' || 'c'", "String:b"},
		{"'' && 'b' || 'c'", "String:c"},
		{"NaN || 'x'", "String:x"},
		{"NaN && 'x'", "Number:NaN"},
		{"'0' || 'x'", "String:0"}, // the string "0" is truthy
		{"matrix.node || 'x'", "Array:Array"},
		{"matrix.obj && 'x'", "String:x"},
		{"fromJSON('[]') && 'x'", "String:x"},
		{"fromJSON('{}') || 'x'", "Object:Object"},
		// Short-circuit: the failing call is never evaluated.
		{"true || fromJSON('')", "Boolean:true"},
		{"false && fromJSON('')", "Boolean:false"},
		{"false || fromJSON('')", "ERR empty input"},
		{"true && fromJSON('')", "ERR empty input"},
		// Comparisons evaluate both sides.
		{"false == fromJSON('')", "ERR empty input"},
		{"fromJSON('') == false", "ERR empty input"},
		// Precedence: && binds tighter than ||, ! tighter than comparison.
		{"true || false && false", "Boolean:true"},
		{"(true || false) && false", "Boolean:false"},
		{"false && true || true", "Boolean:true"},
		{"1 == 1 == true", "Boolean:true"},
		{"1 < 2 == true", "Boolean:true"},
		{"true == 1 < 2", "Boolean:true"},
		{"!1 == 2", "Boolean:false"},
		{"!(1 == 2)", "Boolean:true"},
		{"!!1", "Boolean:true"},
		{"!0", "Boolean:true"},
		{"!''", "Boolean:true"},
		{"!'0'", "Boolean:false"},
		{"!null", "Boolean:true"},
		{"!NaN", "Boolean:true"},
		{"!matrix.node", "Boolean:false"},
		{"!matrix.obj", "Boolean:false"},
		{"!github.event.commits.*", "Boolean:false"},
	})
}

func TestEvalAccess(t *testing.T) {
	runEvalCases(t, []evalCase{
		{"github.event_name", "String:push"},
		{"GITHUB.EVENT_NAME", "String:push"},
		{"GitHub.Event.Number", "Number:12"},
		{"github['event_name']", "String:push"},
		{"github['EVENT_NAME']", "String:push"},
		{"github[ 'event_name' ]", "String:push"},
		{"(github).event_name", "String:push"},
		{"(github.event).number", "Number:12"},
		{"github.nope", "Null:"},
		{"github.nope.deeper", "Null:"},
		{"github.nope[0]", "Null:"},
		{"github.nope.*", "Array:Array"},
		{"github.event.commits[0].id", "String:1"},
		{"github.event.commits[2].id", "String:3"},
		{"github.event.commits[3]", "Null:"},
		{"github.event.commits[-1]", "Null:"},
		{"github.event.commits[1.9].id", "String:2"},
		{"github.event.commits['1'].id", "String:2"},
		{"github.event.commits['1.9'].id", "String:2"},
		{"github.event.commits[' 1 '].id", "String:2"},
		{"github.event.commits[true].id", "String:2"},
		{"github.event.commits[false].id", "String:1"},
		{"github.event.commits[null].id", "String:1"},
		{"github.event.commits[0x1].id", "String:2"},
		{"github.event.commits[NaN]", "Null:"},
		{"github.event.commits[Infinity]", "Null:"},
		{"github.event.commits['a']", "Null:"},
		{"github.event.commits[github]", "Null:"},
		{"github.event.commits[0]['author']['name']", "String:Ann"},
		{"github.event.commits[0].author.name", "String:Ann"},
		{"github.event.commits[2].author.name", "Null:"},
		{"github.event.commits[1e20]", "Null:"},
		// Index by number or boolean on a dictionary uses the string form.
		{"steps[10].outputs.r", "String:ten"},
		{"steps['10'].outputs.r", "String:ten"},
		// After an identifier, ".10" is not a number: the lexer reads a DOT and then "10.outputs.r" as one number-like word.
		{"steps.10.outputs.r", "PARSE Unexpected symbol: '10.outputs.r'. Located at position 7 within expression: steps.10.outputs.r"},
		{"steps[2].outputs.r", "String:two"},
		{"matrix.obj[0]", "Null:"},
		{"matrix.node['0']", "Number:14"},
		{"matrix.node[0] == 14", "Boolean:true"},
		{"matrix.ver", "String:3.10"},
		{"matrix.ver == 3.1", "Boolean:true"},
		{"matrix.ver == '3.10'", "Boolean:true"},
		// Indexing a non-container gives null (or an empty filter with *).
		{"'abc'[0]", "PARSE Unexpected symbol: '['"},
		{"(1)[0]", "Null:"},
		{"(true).a", "Null:"},
		{"(null).a", "Null:"},
		{"(1).*", "Array:Array"},
		{"toJSON((1).*)", "String:[]"},
		// Missing contexts at evaluation time are null.
		{"toJSON(github.event.commits[0].added)", "String:[\n  \"a.txt\"\n]"},
	})
}

func TestEvalFilters(t *testing.T) {
	runEvalCases(t, []evalCase{
		{"toJSON(github.event.commits.*.id)", "String:[\n  \"1\",\n  \"2\",\n  \"3\"\n]"},
		{"toJSON(github.event.commits[*].id)", "String:[\n  \"1\",\n  \"2\",\n  \"3\"\n]"},
		// Elements without the property are skipped.
		{"toJSON(github.event.commits.*.author.name)", "String:[\n  \"Ann\",\n  \"Bob\"\n]"},
		// ".*" on a filtered array flattens one level of arrays and objects.
		{"toJSON(github.event.commits.*.added.*)", "String:[\n  \"a.txt\",\n  \"b\",\n  \"c\"\n]"},
		{"toJSON(github.event.commits.*.added[0])", "String:[\n  \"a.txt\",\n  \"b\"\n]"},
		{"toJSON(github.event.commits.*.added[1])", "String:[\n  \"c\"\n]"},
		{"toJSON(github.event.nested.*.*)", "String:[\n  1,\n  2,\n  3,\n  4,\n  5,\n  6\n]"},
		{"toJSON(github.event.nested.*[1])", "String:[\n  2,\n  5\n]"},
		{"toJSON(github.event.nested[*][0])", "String:[\n  1,\n  3,\n  4\n]"},
		{"toJSON(github.event.nested.*.*.*)", "String:[]"},
		{"toJSON(matrix.obj.*)", "String:[\n  \"v\"\n]"},
		{"toJSON(matrix.node.*)", "String:[\n  14,\n  16,\n  18\n]"},
		{"toJSON(matrix.nothing.*)", "String:[]"},
		{"toJSON(inputs.empty.*)", "String:[]"},
		{"toJSON(steps.*.outputs.r)", "String:[\n  \"two\",\n  \"ten\"\n]"},
		{"toJSON(fromJSON('{\"a\":{\"x\":1},\"b\":{\"x\":2}}').*.x)", "String:[\n  1,\n  2\n]"},
		{"toJSON(fromJSON('[1,2]').*.x)", "String:[]"},
		{"toJSON(fromJSON('[{\"a\":[1,2]},{\"a\":[3]}]').*.a.*)", "String:[\n  1,\n  2,\n  3\n]"},
		{"contains(github.event.commits.*.message, 'FIX: BUG')", "Boolean:true"},
		{"contains(github.event.commits.*.id, 2)", "Boolean:true"},
		{"contains(github.event.commits.*.id, 4)", "Boolean:false"},
		{"contains(matrix.node, 16)", "Boolean:true"},
		{"contains(matrix.node, '16')", "Boolean:true"},
		{"contains(matrix.node, 17)", "Boolean:false"},
		{"join(matrix.node.*, '.')", "String:14.16.18"},
	})
}

func TestEvalBuiltinFunctions(t *testing.T) {
	runEvalCases(t, []evalCase{
		// contains
		{"contains('abc', 'B')", "Boolean:true"},
		{"contains('abc', '')", "Boolean:true"},
		{"contains('', '')", "Boolean:true"},
		{"contains('', 'a')", "Boolean:false"},
		{"contains(null, '')", "Boolean:true"},
		{"contains(1234, 23)", "Boolean:true"},
		{"contains(true, 'RU')", "Boolean:true"},
		{"contains(github, 'a')", "Boolean:false"},
		{"contains('a', github)", "Boolean:false"},
		{"contains(fromJSON('[]'), null)", "Boolean:false"},
		{"contains(fromJSON('[1,\"2\",null]'), null)", "Boolean:true"},
		{"contains(fromJSON('[1,\"2\",null]'), '')", "Boolean:true"},
		{"contains(fromJSON('[[1]]'), fromJSON('[1]'))", "Boolean:false"},
		{"contains('İ', 'i')", "Boolean:false"},      // OrdinalIgnoreCase: İ is not i
		{"contains('ß', 'ss')", "Boolean:false"},     // no ß folding
		{"contains('\u212a', 'k')", "Boolean:false"}, // Kelvin sign
		{"contains('ǅ', 'ǆ')", "Boolean:true"},
		{"contains('A\u03a3', 'a\u03c2')", "Boolean:true"},
		{"contains('K', 'k')", "Boolean:true"}, // KELVIN SIGN lower-cases to k
		{"contains()", "PARSE Too few parameters supplied: 'contains'"},
		{"contains(1)", "PARSE Too few parameters supplied: 'contains'"},
		{"contains(1, 2, 3)", "PARSE Too many parameters supplied: 'contains'"},

		// startsWith, endsWith
		{"startsWith('abc', 'AB')", "Boolean:true"},
		{"startsWith('abc', '')", "Boolean:true"},
		{"startsWith(null, '')", "Boolean:true"},
		{"startsWith(123, 12)", "Boolean:true"},
		{"startsWith(github, 'a')", "Boolean:false"},
		{"startsWith('a', github)", "Boolean:false"},
		{"startsWith('ß', 'ss')", "Boolean:false"}, // no ß folding
		{"startsWith('ı', 'I')", "Boolean:false"},
		{"endsWith('abc', 'BC')", "Boolean:true"},
		{"endsWith('abc', 'abcd')", "Boolean:false"},
		{"endsWith(1.50, '5')", "Boolean:true"},
		{"endsWith(1.50, 5)", "Boolean:true"},
		{"endsWith('abc')", "PARSE Too few parameters supplied: 'endsWith'"},
		{"STARTSWITH('abc', 'a')", "Boolean:true"},

		// join
		{"join('a')", "String:a"},
		{"join('a', 'b')", "String:a"},
		{"join(null)", "String:"},
		{"join(1.50)", "String:1.5"},
		{"join(github)", "String:"},
		{"join(fromJSON('[]'))", "String:"},
		{"join(fromJSON('[1,2,3]'))", "String:1,2,3"},
		{"join(fromJSON('[1,2,3]'), '')", "String:123"},
		{"join(fromJSON('[1,2,3]'), ', ')", "String:1, 2, 3"},
		{"join(fromJSON('[1,2,3]'), 1)", "String:11213"},
		{"join(fromJSON('[1,2,3]'), null)", "String:123"},
		{"join(fromJSON('[1,2,3]'), github)", "String:1,2,3"}, // a non-primitive separator is ignored
		{"join(fromJSON('[null,true,\"a\",1.5,[1],{}]'))", "String:,true,a,1.5,Array,Object"},
		{"join()", "PARSE Too few parameters supplied: 'join'"},
		{"join(1, 2, 3)", "PARSE Too many parameters supplied: 'join'"},

		// toJSON
		{"toJSON(null)", "String:null"},
		{"toJSON(true)", "String:true"},
		{"toJSON(1)", "String:1"},
		{"toJSON(NaN)", "String:NaN"},
		{"toJSON(Infinity)", "String:Infinity"},
		{"toJSON(-0)", "String:0"},
		{"toJSON('x')", "String:\"x\""},
		{"toJSON(1e21)", "String:1E+21"},
		{"toJSON(1e-7)", "String:1E-07"},
		{"toJSON(0.1)", "String:0.1"},
		{"toJSON(fromJSON('[]'))", "String:[]"},
		{"toJSON(fromJSON('{}'))", "String:{}"},
		{"toJSON(fromJSON('[[],{}]'))", "String:[\n  [],\n  {}\n]"},
		{"toJSON(1, 2)", "PARSE Too many parameters supplied: 'toJSON'"},
		{"toJSON()", "PARSE Too few parameters supplied: 'toJSON'"},
		{"fromJSON(toJSON(github.event.commits[0])).author.name", "String:Ann"},

		// fromJSON
		{"fromJSON('')", "ERR empty input"},
		{"fromJSON('  ')", "ERR empty input"},
		{"fromJSON('\n')", "ERR empty input"},
		{"fromJSON(null)", "ERR empty input"},
		{"fromJSON('{')", "ERR Error parsing JSON when evaluating fromJson"},
		{"fromJSON('[1,]')", "ERR Error parsing JSON when evaluating fromJson"},
		{"fromJSON('{a:1}')", "ERR Error parsing JSON when evaluating fromJson"},
		{"fromJSON('01')", "ERR Error parsing JSON when evaluating fromJson"},
		{"fromJSON('NaN')", "ERR Error parsing JSON when evaluating fromJson"},
		{"fromJSON('1 2')", "ERR Error parsing JSON when evaluating fromJson"},
		{"fromJSON(' 1 ')", "Number:1"},
		{"fromJSON('-1.5e2')", "Number:-150"},
		{"fromJSON('1e999')", "Number:Infinity"},
		{"fromJSON('\"\\u0041\"')", "String:A"},
		{"fromJSON('\"\\ud83d\\ude00\"')", "String:😀"},
		{"fromJSON(1)", "Number:1"},
		{"fromJSON(true)", "Boolean:true"},
		{"fromJSON(matrix.node)", "ERR Error parsing JSON when evaluating fromJson"}, // the string "Array"
		{"fromJSON(1, 2)", "PARSE Too many parameters supplied: 'fromJSON'"},

		// format
		{"format('a')", "String:a"},
		{"format('')", "String:"},
		{"format(null)", "String:"},
		{"format(1)", "String:1"},
		{"format('{0}', 1)", "String:1"},
		{"format('{0}{0}', 'x')", "String:xx"},
		{"format('{0}{1}{0}', 'a', 'b')", "String:aba"},
		{"format('{{')", "String:{"},
		{"format('}}')", "String:}"},
		{"format('{{}}')", "String:{}"},
		{"format('{{0}}', 1)", "String:{0}"},
		{"format('{{{0}}}', 1)", "String:{1}"},
		{"format('{0}', 'a', 'b')", "String:a"}, // extra arguments are fine
		{"format('{0:x}', 1)", "String:1"},      // a format specifier is skipped
		{"format('{0 anything}', 1)", "String:1"},
		{"format('{00}', 1)", "String:1"},
		{"format('{01}', 1, 2)", "String:2"},
		{"format('{0}', null)", "String:"},
		{"format('{0}', true)", "String:true"},
		{"format('{0}', 0.1)", "String:0.1"},
		{"format('{0}', github)", "String:Object"},
		{"format('{0}', matrix.node)", "String:Array"},
		{"format('{0}-{1}', 1, 'a')", "String:1-a"},
		{"format('x{0}y', '😀')", "String:x😀y"},
		{"format('{1}', 1)", "ERR The following format string references more arguments than were supplied: {1}"},
		{"format('{0}')", "ERR The following format string references more arguments than were supplied: {0}"},
		{"format('{', 1)", "ERR The following format string is invalid: {"},
		{"format('}', 1)", "ERR The following format string is invalid: }"},
		{"format('{}', 1)", "ERR The following format string is invalid: {}"},
		{"format('{ 0}', 1)", "ERR The following format string is invalid: { 0}"},
		{"format('{-1}', 1)", "ERR The following format string is invalid: {-1}"},
		{"format('{a}', 1)", "ERR The following format string is invalid: {a}"},
		{"format('{0', 1)", "ERR The following format string is invalid: {0"},
		{"format('0}', 1)", "ERR The following format string is invalid: 0}"},
		{"format('{{0}', 1)", "ERR The following format string is invalid: {{0}"},
		{"format('{0}}', 1)", "ERR The following format string is invalid: {0}}"},
		{"format('{12345678901234567890123}', 1)", "ERR The following format string references more arguments than were supplied: {12345678901234567890123}"},
		{"format()", "PARSE Too few parameters supplied: 'format'"},

		// case
		{"case(true, 'a', 'b')", "String:a"},
		{"case(false, 'a', 'b')", "String:b"},
		{"case(false, 1, true, 2, 3)", "Number:2"},
		{"case(false, 1, false, 2, 3)", "Number:3"},
		{"case(true, 1, true, 2, 3)", "Number:1"},
		{"case(github.event_name == 'push', 'a', github.event_name == 'pr', 'b', 'c')", "String:a"},
		{"CASE(true, matrix.node, 2)", "Array:Array"},
		{"case(1, 2, 3)", "ERR case predicate must evaluate to a boolean value"},
		{"case('true', 2, 3)", "ERR case predicate must evaluate to a boolean value"},
		{"case(null, 2, 3)", "ERR case predicate must evaluate to a boolean value"},
		{"case(true, 1, 2, 3)", "PARSE Even number of parameters supplied, requires an odd number of parameters: 'case'"},
		{"case(true, 1)", "PARSE Too few parameters supplied: 'case'"},
		{"case()", "PARSE Too few parameters supplied: 'case'"},
		// Predicates after the first true one are not checked...
		{"case(true, 1, 'bad', 2, 3)", "Number:1"},
		// ...but earlier ones are.
		{"case('bad', 1, true, 2, 3)", "ERR case predicate must evaluate to a boolean value"},
		// The default is returned as is, whatever it is.
		{"case(false, 1, null)", "Null:"},
	})
}

func TestEvalErrorMessages(t *testing.T) {
	runEvalCases(t, []evalCase{
		{"", "PARSE Unexpected end of expression: 'x'"}, // replaced below: the empty expression is not an error
	}[:0])
	cases := []evalCase{
		{"1 +", "PARSE Unexpected symbol: '+'. Located at position 3 within expression: 1 +"},
		{"a b", "PARSE Unrecognized named-value: 'a'"},
		{"github github", "PARSE Unexpected symbol: 'github'"},
		{"github.event_name ==", "PARSE Unexpected end of expression: '=='"},
		{"== 1", "PARSE Unexpected symbol: '=='"},
		{"(github.event_name", "PARSE Unexpected end of expression: 'event_name'"},
		{"github.event_name)", "PARSE Unexpected symbol: ')'"},
		{"contains('a',", "PARSE Unexpected end of expression: ','"},
		{"contains('a'", "PARSE Unexpected symbol: 'EOF'"},
		{"contains('a' 'b')", "PARSE Unexpected symbol: 'b'"},
		{"foo(", "PARSE Unexpected end of expression: '('"},
		{"foo()", "PARSE Unrecognized function: 'foo'"},
		{"foo", "PARSE Unrecognized named-value: 'foo'"},
		{"unknown(1)", "PARSE Unrecognized function: 'unknown'"},
		{"success()", "PARSE Unrecognized function: 'success'"}, // not declared in these tests
		{"(", "PARSE Unexpected end of expression: '('"},
		{")", "PARSE Unexpected symbol: ')'"},
		{"()", "PARSE Unexpected symbol: ')'"},
		{"!", "PARSE Unexpected end of expression: '!'"},
		{"1 !", "PARSE Unexpected symbol: '!'"},
		{"[1]", "PARSE Unexpected symbol: '['"},
		{"github.[0]", "PARSE Unexpected symbol: '['"},
		{"github.", "PARSE Unexpected symbol: 'EOF'"},
		{"github[", "PARSE Unexpected end of expression: '['"},
		{"github[]", "PARSE Unexpected symbol: ']'"},
		{"github[*", "PARSE Unexpected symbol: 'EOF'"},
		{"github.true", "Null:"}, // keywords are plain identifiers after a dot
		{"env.true", "Null:"},
		{"1 = 1", "PARSE Unexpected symbol: '='. Located at position 3 within expression: 1 = 1"},
		{"1 & 1", "PARSE Unexpected symbol: '&'. Located at position 3 within expression: 1 & 1"},
		{"1 | 1", "PARSE Unexpected symbol: '|'. Located at position 3 within expression: 1 | 1"},
		{"1 + 1", "PARSE Unexpected symbol: '+'. Located at position 3 within expression: 1 + 1"},
		{"a-b", "PARSE Unrecognized named-value: 'a-b'"},
		{"1a", "PARSE Unexpected symbol: '1a'. Located at position 1 within expression: 1a"},
		{"'unterminated", "PARSE Unexpected symbol: ''unterminated'. Located at position 1 within expression: 'unterminated"},
		{"@", "PARSE Unexpected symbol: '@'. Located at position 1 within expression: @"},
		{"${{ 1 }}", "PARSE Unexpected symbol: '${{'. Located at position 1 within expression: ${{ 1 }}"},
		{"1 \u00a0 2", "PARSE Unexpected symbol: '\u00a0'. Located at position 3 within expression: 1 \u00a0 2"},
	}
	runEvalCases(t, cases)
}

func TestEmptyExpression(t *testing.T) {
	for _, s := range []string{"", " ", "\n", " \t\r\n "} {
		node, err := Parse(s, nil, nil)
		if err != nil || node != nil {
			t.Errorf("Parse(%q) = %v, %v; want nil, nil", s, node, err)
		}
	}
	if _, err := Evaluate(context.Background(), nil, EvalContext{}); err == nil {
		t.Error("Evaluate(nil) succeeded")
	}
}

func TestParseErrorPositions(t *testing.T) {
	tests := []struct {
		src       string
		line, col int
		msg       string
	}{
		{"1 ==\n 2 ===\n 3", 1, 5, "Unexpected symbol: '='. Located at position 11 within expression: 1 ==\n 2 ===\n 3"},
		{"true &&\n  false ||\n  nope", 2, 2, "Unrecognized named-value: 'nope'"},
		{"\n\n  bogus", 2, 2, "Unrecognized named-value: 'bogus'"},
		{"github github", 0, 7, "Unexpected symbol: 'github'"},
		{"  )  ", 0, 2, "Unexpected symbol: ')'"},
		{"contains('a',", 0, 12, "Unexpected end of expression: ','"},
		{"foo()", 0, 0, "Unrecognized function: 'foo'"},
		{"1 && foo()", 0, 5, "Unrecognized function: 'foo'"},
		{"'a'\n 'b'\n  zzz", 1, 1, "Unexpected symbol: 'b'"},
	}
	for _, tc := range tests {
		_, err := Parse(tc.src, testContextNames, nil)
		var pe *Error
		if !errors.As(err, &pe) {
			t.Errorf("Parse(%q): error %v is not *Error", tc.src, err)
			continue
		}
		if pe.Message != tc.msg || pe.Pos.Line != tc.line || pe.Pos.Column != tc.col {
			t.Errorf("Parse(%q) = %q at %v; want %q at %d:%d", tc.src, pe.Message, pe.Pos, tc.msg, tc.line, tc.col)
		}
	}
}

func TestParseErrorTypes(t *testing.T) {
	tests := []struct {
		src  string
		want ErrorType
	}{
		{"1 ==", ErrorUnexpectedEndOfExpression},
		{")", ErrorUnexpectedSymbol},
		{"nope", ErrorUnrecognizedContext},
		{"nope()", ErrorUnrecognizedFunction},
		{"contains(1)", ErrorTooFewParameters},
		{"contains(1,2,3)", ErrorTooManyParameters},
		{"case(true, 1, 2, 3)", ErrorEvenParameters},
		{strings.Repeat("(", 60) + "1" + strings.Repeat(")", 60), ErrorExceededMaxDepth},
		{"1 = 1", ErrorUnexpectedSymbol},
	}
	for _, tc := range tests {
		_, err := Parse(tc.src, nil, nil)
		var pe *Error
		if !errors.As(err, &pe) || pe.Type != tc.want {
			t.Errorf("Parse(%.30q): got %#v, want type %d", tc.src, err, tc.want)
		}
	}
}

func TestLimits(t *testing.T) {
	deepParens := func(n int) string { return strings.Repeat("(", n) + "1" + strings.Repeat(")", n) }
	tests := []struct {
		name string
		src  string
		ok   bool
	}{
		// Each "(" costs one level, plus one for the outermost expression.
		{"49 parens", deepParens(49), true},
		{"50 parens", deepParens(50), false},
		{"49 nots", strings.Repeat("!", 49) + "true", true},
		{"50 nots", strings.Repeat("!", 50) + "true", false},
		{"49 calls", strings.Repeat("format(", 49) + "'x'" + strings.Repeat(")", 49), true},
		{"50 calls", strings.Repeat("format(", 50) + "'x'" + strings.Repeat(")", 50), false},
		{"long dotted path", "github" + strings.Repeat(".event", 49), true},
		{"too long dotted path", "github" + strings.Repeat(".event", 50), false},
		{"long flat ||", "1" + strings.Repeat(" || 1", 3000), true},
		{"long flat ==", "1" + strings.Repeat(" == 1", 3000), true},
	}
	for _, tc := range tests {
		_, err := Parse(tc.src, testContextNames, nil)
		if (err == nil) != tc.ok {
			t.Errorf("%s: err = %v, want ok=%v", tc.name, err, tc.ok)
		}
		var pe *Error
		if err != nil && errors.As(err, &pe) && pe.Type == ErrorExceededMaxDepth && pe.Message != "Exceeded max expression depth 50: '1'" && !strings.HasPrefix(pe.Message, "Exceeded max expression depth 50: '") {
			t.Errorf("%s: message %q", tc.name, pe.Message)
		}
	}

	// Length: counted in UTF-16 code units, error is the bare token.
	long := "'" + strings.Repeat("a", MaxExpressionLength-2) + "'"
	if _, err := Parse(long, nil, nil); err != nil {
		t.Errorf("expression of exactly %d characters: %v", MaxExpressionLength, err)
	}
	_, err := Parse(long+" ", nil, nil)
	if err == nil || err.Error() != "ErrorExceededMaxLength" {
		t.Errorf("expression of %d characters: err = %v", MaxExpressionLength+1, err)
	}
	// 10501 emoji are 21002 UTF-16 units but only 10501 runes.
	emoji := "'" + strings.Repeat("😀", 10500) + "'"
	if _, err := Parse(emoji, nil, nil); err == nil {
		t.Errorf("a string of 10500 emoji (21002 UTF-16 units) was accepted")
	}
	emoji = "'" + strings.Repeat("😀", 10499) + "'"
	if _, err := Parse(emoji, nil, nil); err != nil {
		t.Errorf("a string of 10499 emoji (21000 UTF-16 units) was rejected: %v", err)
	}
}

func TestDepthMessage(t *testing.T) {
	_, err := Parse(strings.Repeat("(", 60)+"1"+strings.Repeat(")", 60), nil, nil)
	want := "Exceeded max expression depth 50: '('"
	if err == nil || err.Error() != want {
		t.Errorf("got %v, want %q", err, want)
	}
}

func TestExtensionFunctions(t *testing.T) {
	funcs := []FuncInfo{
		{Name: "hashFiles", MinArgs: 1, MaxArgs: -1},
		{Name: "success", MinArgs: 0, MaxArgs: 0},
		{Name: "bounded", MinArgs: 1, MaxArgs: 2},
		{Name: "contains", MinArgs: 0, MaxArgs: 0}, // built-ins win for validation
	}
	for _, c := range []struct{ src, err string }{
		{"hashFiles('a')", ""},
		{"HASHFILES('a', 'b', 'c', 'd')", ""},
		{"hashFiles()", "Too few parameters supplied: 'hashFiles'"},
		{"success()", ""},
		{"success(1)", "Too many parameters supplied: 'success'"},
		{"bounded(1, 2)", ""},
		{"bounded(1, 2, 3)", "Too many parameters supplied: 'bounded'"},
		{"contains()", "Too few parameters supplied: 'contains'"},
		{"nothere()", "Unrecognized function: 'nothere'"},
	} {
		_, err := Parse(c.src, nil, funcs)
		switch {
		case c.err == "" && err != nil:
			t.Errorf("%s: %v", c.src, err)
		case c.err != "" && (err == nil || err.Error() != c.err):
			t.Errorf("%s: err = %v, want %q", c.src, err, c.err)
		}
	}

	calls := 0
	ctx := EvalContext{
		Contexts: map[string]Value{"G": String("v")},
		Funcs: map[string]Func{
			"HashFiles": func(_ context.Context, args []Value) (Value, error) {
				calls++
				return String("h:" + CoerceString(args[0])), nil
			},
			"success": func(context.Context, []Value) (Value, error) { return Boolean(true), nil },
			"boom":    func(context.Context, []Value) (Value, error) { return nil, errors.New("kaput") },
			// An extra function replaces a built-in of the same name.
			"contains": func(context.Context, []Value) (Value, error) { return String("mine"), nil },
		},
	}
	node, err := Parse("hashFiles(g) == 'h:v' && success() && contains('a','b')", []string{"g"}, funcs)
	if err != nil {
		t.Fatal(err)
	}
	v, err := Evaluate(context.Background(), node, ctx)
	if err != nil || CoerceString(v) != "mine" || calls != 1 {
		t.Errorf("got %v, %v, %d calls", v, err, calls)
	}
	node, err = Parse("boom()", nil, []FuncInfo{{Name: "boom"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Evaluate(context.Background(), node, ctx); err == nil || err.Error() != "kaput" {
		t.Errorf("function error not propagated: %v", err)
	}
	// A declared function without an implementation is an evaluation error.
	node, _ = Parse("success()", nil, funcs)
	if _, err = Evaluate(context.Background(), node, EvalContext{}); err == nil {
		t.Error("calling an unimplemented function succeeded")
	}
}

func TestContextNames(t *testing.T) {
	node, err := Parse("GitHub.Ref", []string{"github"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"github", "GITHUB", "GitHub"} {
		d := NewDictionary()
		d.Add("ref", String("main"))
		v, err := Evaluate(context.Background(), node, EvalContext{Contexts: map[string]Value{key: d}})
		if err != nil || CoerceString(v) != "main" {
			t.Errorf("context key %q: %v, %v", key, v, err)
		}
	}
	// A declared but absent context is null.
	v, err := Evaluate(context.Background(), node, EvalContext{})
	if err != nil || v.Kind() != KindNull {
		t.Errorf("absent context: %v, %v", v, err)
	}
	// Names that were not declared to Parse are errors, even if present at evaluation.
	if _, err := Parse("secrets.X", []string{"github"}, nil); err == nil || err.Error() != "Unrecognized named-value: 'secrets'" {
		t.Errorf("undeclared context: %v", err)
	}
	// A nil Value in a context behaves as null.
	node, _ = Parse("github.a.b", []string{"github"}, nil)
	if v, err := Evaluate(context.Background(), node, EvalContext{Contexts: map[string]Value{"github": nil}}); err != nil || v.Kind() != KindNull {
		t.Errorf("nil context value: %v, %v", v, err)
	}
	var nilDict *Dictionary
	var nilArr *Array
	node, _ = Parse("(github)[0] || x", []string{"github", "x"}, nil)
	for _, bad := range []Value{nilDict, nilArr} {
		if _, err := Evaluate(context.Background(), node, EvalContext{Contexts: map[string]Value{"github": bad}}); err != nil {
			t.Errorf("typed nil %T: %v", bad, err)
		}
	}
}

func TestEvaluateDoesNotMutateContext(t *testing.T) {
	ctxs := testContexts(t)
	before := map[string]string{}
	for k, v := range ctxs {
		before[k] = ToJSON(v)
	}
	for _, src := range []string{"github.event.commits.*", "github.event.commits.*.added.*", "github.*", "matrix.node.*", "toJSON(steps.*)"} {
		node, err := Parse(src, testContextNames, nil)
		if err != nil {
			t.Fatal(err)
		}
		v, err := Evaluate(context.Background(), node, EvalContext{Contexts: ctxs})
		if err != nil {
			t.Fatal(err)
		}
		if a, ok := v.(*Array); ok {
			a.Append(String("tampered"))
		}
	}
	for k, v := range ctxs {
		if ToJSON(v) != before[k] {
			t.Errorf("context %s was modified", k)
		}
	}
}

// TestFloatCoercion checks the string form of numbers against the C# runner:
// Double.ToString("G15", InvariantCulture) (ExpressionConstants.NumberFormat,
// EvaluationResult.ConvertToString). Each expectation was obtained by running
// the runner's own code (gen/runner, --g15), and numberToCoercedString agrees
// with it on 200 000 random doubles.
func TestFloatCoercion(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{0, "0"},
		{math.Copysign(0, -1), "0"}, // see numberToCoercedString
		{1, "1"},
		{-1, "-1"},
		{1.5, "1.5"},
		{0.1, "0.1"},
		{0.30000000000000004, "0.3"},
		{1.005, "1.005"},
		{100, "100"},
		{123456789012345, "123456789012345"},
		{999999999999999, "999999999999999"},
		{1e14, "100000000000000"},
		{1e15, "1E+15"},
		{1e16, "1E+16"},
		{1e20, "1E+20"},
		{1e21, "1E+21"},
		{123456789012345680000, "1.23456789012346E+20"},
		{1234567890123456, "1.23456789012346E+15"},
		{0.0001, "0.0001"},
		{0.00001, "1E-05"},
		{1.5e-5, "1.5E-05"},
		{1e-6, "1E-06"},
		{1e-7, "1E-07"},
		{5e-16, "5E-16"},
		{-1e-16, "-1E-16"},
		{9.999999999999995e-5, "0.0001"},
		{99999999999999.95, "100000000000000"},
		{999999999999999.5, "1E+15"},
		{123456789.123456789, "123456789.123457"},
		{123456789012345.5, "123456789012346"},
		{math.NaN(), "NaN"},
		{math.Inf(1), "Infinity"},
		{math.Inf(-1), "-Infinity"},
		{2.5, "2.5"},
		{-0.25, "-0.25"},
		{1.7976931348623157e308, "1.79769313486232E+308"},
		{5e-324, "4.94065645841247E-324"},
	}
	for _, tc := range tests {
		if got := CoerceString(Number(tc.in)); got != tc.want {
			t.Errorf("CoerceString(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestJSNumberToString(t *testing.T) {
	one, two := 0.1, 0.2
	sum := one + two // not a constant expression: Go would fold 0.1+0.2 exactly
	tests := []struct {
		in   float64
		want string
	}{
		{0, "0"}, {1, "1"}, {-1.5, "-1.5"}, {100, "100"}, {1e20, "100000000000000000000"}, {1e21, "1e+21"},
		{1.5e21, "1.5e+21"}, {1e-6, "0.000001"}, {1e-7, "1e-7"}, {1.5e-7, "1.5e-7"}, {123456.789, "123456.789"},
		{0.000123, "0.000123"}, {5e-324, "5e-324"}, {1.7976931348623157e308, "1.7976931348623157e+308"},
		{-1e-7, "-1e-7"}, {sum, "0.30000000000000004"}, {math.NaN(), "NaN"}, {math.Inf(-1), "-Infinity"},
	}
	for _, tc := range tests {
		if got := jsNumberToString(tc.in); got != tc.want {
			t.Errorf("jsNumberToString(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestJSToNumber(t *testing.T) {
	nan := math.NaN()
	tests := []struct {
		in   string
		want float64
	}{
		{"", 0}, {"  ", 0}, {"\n\t", 0}, {"1", 1}, {" 1 ", 1}, {"\u00a01\u2003", 1}, {"\ufeff1", 1}, {"+1", 1}, {"-1", -1},
		{"1.", 1}, {".5", 0.5}, {"-.5", -0.5}, {"1e3", 1000}, {"1E-3", 0.001}, {"1e+3", 1000}, {"0x10", 16}, {"0X1f", 31},
		{"0o17", 15}, {"0b101", 5}, {"0x", nan}, {"0xg", nan}, {"-0x10", nan}, {"+0x10", nan}, {"0b2", nan}, {"1_000", nan},
		{"Infinity", math.Inf(1)}, {"-Infinity", math.Inf(-1)}, {"+Infinity", math.Inf(1)}, {"infinity", nan}, {"inf", nan},
		{"NaN", nan}, {"nan", nan}, {"abc", nan}, {"1e", nan}, {"1e+", nan}, {".", nan}, {"e5", nan}, {"1 2", nan}, {"1,5", nan},
		{"00", 0}, {"007", 7}, {"0.0", 0}, {"1e999", math.Inf(1)}, {"-1e999", math.Inf(-1)}, {"0x1fffffffffffff", 9007199254740991},
		{"12345678901234567890", 12345678901234567890}, {"٣", nan}, {"--1", nan}, {"+-1", nan}, {"1e1.5", nan}, {"0x1.8", nan},
	}
	for _, tc := range tests {
		got := jsToNumber(tc.in)
		if got != tc.want && !(math.IsNaN(got) && math.IsNaN(tc.want)) {
			t.Errorf("jsToNumber(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestJSCasing(t *testing.T) {
	upper := map[string]string{"abc": "ABC", "ß": "SS", "ŉ": "ʼN", "ǆ": "Ǆ", "ı": "I", "ﬃ": "FFI", "ὒ": "Υ̓̀", "ᾀ": "ἈΙ", "σ": "Σ", "ς": "Σ", "😀": "😀", "é": "É"}
	for in, want := range upper {
		if got := jsToUpper(in); got != want {
			t.Errorf("jsToUpper(%q) = %q, want %q", in, got, want)
		}
	}
	special := map[string]string{"ı": "ı", "ıi": "ıI", "I": "I", "ß": "SS", "aıb": "AıB"}
	for in, want := range special {
		if got := toUpperSpecial(in); got != want {
			t.Errorf("toUpperSpecial(%q) = %q, want %q", in, got, want)
		}
	}
	lower := map[string]string{
		"ABC": "abc", "İ": "i\u0307", "ΑΣ": "ας", "ΑΣΑ": "ασα", "Α Σ": "α σ", "ΑΣ.": "ας.", "Σ": "σ", "ΑΣ'Α": "ασ'α",
		"K": "k", "Å": "å", "😀": "😀", "Ǆ": "ǆ", "ΑΣΣ": "ασς", "Α.Σ": "α.ς",
	}
	for in, want := range lower {
		if got := jsToLower(in); got != want {
			t.Errorf("jsToLower(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCompareUTF16(t *testing.T) {
	// U+10000 is the surrogate pair D800 DC00, which sorts before U+E000.
	if got := compareUTF16("\U00010000", "\ue000"); got >= 0 {
		t.Errorf("compareUTF16(U+10000, U+E000) = %d, want < 0", got)
	}
	if got := compareUTF16("\uffff", "\U00010000"); got <= 0 {
		t.Errorf("compareUTF16(U+FFFF, U+10000) = %d, want > 0", got)
	}
	for _, c := range []struct {
		a, b string
		want int
	}{{"a", "a", 0}, {"a", "b", -1}, {"b", "a", 1}, {"a", "ab", -1}, {"ab", "a", 1}, {"", "", 0}, {"", "a", -1}} {
		if got := compareUTF16(c.a, c.b); got != c.want {
			t.Errorf("compareUTF16(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
	// And through the operators.
	for _, c := range evalCasesUTF16 {
		if got := evalString(t, c.expr); got != c.want {
			t.Errorf("%s = %s, want %s", c.expr, got, c.want)
		}
	}
}

var evalCasesUTF16 = []evalCase{
	// The JavaScript reference compares UTF-16 units, where U+10000 (D800 DC00)
	// sorts before U+E000. The runner compares code points: it sorts after.
	{"'\U00010000' < '\ue000'", "Boolean:false"},
	{"'\ue000' > '\U00010000'", "Boolean:false"},
	{"'\U00010000' >= '\ue000'", "Boolean:true"},
	{"'\ue000' < '\U00010000'", "Boolean:true"},
}

func TestTokenString(t *testing.T) {
	toks, err := Lex("a.b == 'x''y' && 1.5")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tk := range toks {
		got = append(got, tk.Type.String())
	}
	want := "IDENTIFIER DOT IDENTIFIER EQUAL_EQUAL STRING AND NUMBER EOF"
	if strings.Join(got, " ") != want {
		t.Errorf("tokens = %v, want %s", got, want)
	}
	if toks[4].Value.(String) != "x'y" || toks[4].Lexeme != "'x''y'" {
		t.Errorf("string token = %#v", toks[4])
	}
	if TokenType(99).String() != "TokenType(99)" {
		t.Error("unknown token type")
	}
}

// The EOF token of the reference is positioned from a stale token start, so
// after a trailing newline its column is negative. Kept as is.
func TestEOFTokenPosition(t *testing.T) {
	toks, err := Lex("a\n")
	if err != nil {
		t.Fatal(err)
	}
	eof := toks[len(toks)-1]
	if eof.Type != TokenEOF || eof.Range.Start != (Position{Line: 1, Column: -1}) || eof.Range.End != (Position{Line: 1, Column: 0}) {
		t.Errorf("EOF token = %+v", eof)
	}
}
