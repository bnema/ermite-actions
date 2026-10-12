// SPDX-License-Identifier: MIT

package expr

import (
	"math"
	"strings"
	"testing"
)

// The accepted and rejected inputs below were established by running
// fromJSON through the runner's own code (gen/runner): Json.NET's reader, not
// JSON.parse.
func TestFromJSONValid(t *testing.T) {
	tests := []struct {
		in   string
		want string // ToJSON of the result
	}{
		{"null", "null"},
		{" true ", "true"},
		{"\t\r\n false \n", "false"},
		{"0", "0"},
		{"-12.5e1", "-125"},
		{"1E2", "100"},
		{"0.1e1", "1"},
		{`"a\u00e9\n"`, `"aé\n"`},
		{`"\ud83d\ude00"`, `"😀"`},
		{`"\/"`, `"/"`},
		{`[]`, `[]`},
		{`{}`, `{}`},
		{`[ 1 , [ ] , { } ]`, "[\n  1,\n  [],\n  {}\n]"},
		// An unpaired surrogate escape becomes U+FFFD.
		{`"\ud800"`, "\"\ufffd\""},
		{`"\udc00x"`, "\"\ufffdx\""},
		{`"\ud800\u0041"`, "\"\ufffdA\""},
		// Repeated keys: the last value, at the first position and spelling;
		// keys compare case-insensitively.
		{`{"a":1,"A":2}`, "{\n  \"a\": 2\n}"},
		{`{"a":1,"a":2}`, "{\n  \"a\": 2\n}"},
		// Document order, array-index keys included.
		{`{"b":1,"1":2,"a":3,"0":4}`, "{\n  \"b\": 1,\n  \"1\": 2,\n  \"a\": 3,\n  \"0\": 4\n}"},
		// Leniency of Json.NET.
		{`{"a":1,}`, "{\n  \"a\": 1\n}"},
		{`{a:1,$b:2,c_d:3}`, "{\n  \"a\": 1,\n  \"$b\": 2,\n  \"c_d\": 3\n}"},
		{`{'a':'x\'y'}`, "{\n  \"a\": \"x'y\"\n}"},
		{`[1,]`, "[\n  1\n]"},
		{`[,1]`, "[\n  \"\",\n  1\n]"},
		{`[1,,2]`, "[\n  1,\n  \"\",\n  2\n]"},
		{"1.", "1"}, {".5", "0.5"}, {"-.5", "-0.5"}, {"1.e2", "100"},
		{"0x10", "16"}, {"0X1f", "31"},
		{"NaN", "NaN"}, {"Infinity", "Infinity"}, {"-Infinity", "-Infinity"},
		{"undefined", `""`},
		{"1 2", "1"}, {"[] []", "[]"}, {"{} x", "{}"}, {`"a""b"`, `"a"`}, {"true false", "true"},
		{"\u00a01", "1"}, {"1\u00a0", "1"}, {"\u00851", "1"},
		{"\"a\nb\"", `"a\nb"`}, {"\"\x01\"", `"\u0001"`},
	}
	for _, tc := range tests {
		v, err := FromJSON([]byte(tc.in))
		if err != nil {
			t.Errorf("FromJSON(%q): %v", tc.in, err)
			continue
		}
		if got := ToJSON(v); got != tc.want {
			t.Errorf("FromJSON(%q) -> %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestFromJSONLeadingZeros: Json.NET reads a non-negative integer with a
// leading zero as octal (Convert.ToInt64(s, 8)), a negative one as decimal, and
// rejects a leading zero before a fraction or an exponent. Established with the
// runner's code.
func TestFromJSONLeadingZeros(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want string // ToJSON, or "" for an error
	}{
		{"010", "8"}, {"050", "40"}, {"00", "0"}, {"007", "7"}, {"0777", "511"}, {"00777", "511"},
		{"08", ""}, {"09", ""}, {"01.5", ""}, {"012e1", ""}, {"00.5", ""}, {"010.0", ""}, {"00e1", ""}, {"010e", ""},
		{"0.5", "0.5"}, {"0e1", "0"}, {"0E1", "0"}, {"0.", "0"}, {"0.e1", "0"}, {"0e", ""},
		{"-010", "-10"}, {"-09", "-9"}, {"-00", "0"}, {"-0777", "-777"}, {"-01.5", "-1.5"}, {"-012e1", "-120"},
		{"[010]", "[\n  8\n]"}, {"[08]", ""}, {`{"a":010}`, "{\n  \"a\": 8\n}"}, {`{"a":08}`, ""}, {`{"a":01.5}`, ""},
		// 64-bit wrap of Convert.ToInt64.
		{"0777777777777777777777", "9.22337203685478E+18"},
		{"01777777777777777777777", "-1"},
		{"07777777777777777777777", ""},
		{"0000000000000000000012345", "5349"},
		{"0000000000000000000012349", ""},
	} {
		v, err := FromJSON([]byte(tc.src))
		switch {
		case tc.want == "" && err == nil:
			t.Errorf("FromJSON(%q) = %s, want an error", tc.src, ToJSON(v))
		case tc.want != "" && err != nil:
			t.Errorf("FromJSON(%q): %v", tc.src, err)
		case err == nil && ToJSON(v) != tc.want:
			t.Errorf("FromJSON(%q) = %s, want %s", tc.src, ToJSON(v), tc.want)
		}
	}
}

// TestFromJSONSlashAndComma: a "/" after a literal is an error when it could
// start a comment (comments are not ported, so it fails consistently), as it is
// for Json.NET after null, true, false, NaN and Infinity and between the values
// of an array or object; after a number or string at the top level the text is
// ignored. A comma where the document should start is an undefined value.
func TestFromJSONSlashAndComma(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want string // ToJSON, or "" for an error
	}{
		{"null/x", ""}, {"null/", ""}, {"true/x", ""}, {"false/x", ""}, {"NaN/x", ""}, {"Infinity/x", ""}, {"-Infinity/x", ""},
		{"undefined/x", ""}, {"null//c", ""}, {"null/*c*/", ""}, {"NaN/*c*/", ""}, {"[null/x]", ""}, {"[null/*c*/]", ""}, {`{"a":null/x}`, ""},
		{"[1/2]", ""}, {"[1 /2]", ""}, {`{"a":1/2}`, ""}, {"[1/*c*/,2]", ""}, {"[1//c\n,2]", ""},
		{"null /x", "null"}, {"1/x", "1"}, {"1/", "1"}, {"1 /", "1"}, {"1.5/x", "1.5"}, {`"a"/x`, `"a"`}, {"[null,1]/x", "[\n  null,\n  1\n]"},
		{",", `""`}, {",1", `""`}, {",,1", `""`}, {",x", `""`}, {" ,1", `""`}, {`,"a"`, `""`}, {",]", `""`},
		{"[,1]", "[\n  \"\",\n  1\n]"}, {"[1,,2]", "[\n  1,\n  \"\",\n  2\n]"}, {"[,]", "[\n  \"\"\n]"},
		{"{,}", ""}, {`{"a":,1}`, ""}, {`{"a":1,,"b":2}`, ""},
	} {
		v, err := FromJSON([]byte(tc.src))
		switch {
		case tc.want == "" && err == nil:
			t.Errorf("FromJSON(%q) = %s, want an error", tc.src, ToJSON(v))
		case tc.want != "" && err != nil:
			t.Errorf("FromJSON(%q): %v", tc.src, err)
		case err == nil && ToJSON(v) != tc.want:
			t.Errorf("FromJSON(%q) = %s, want %s", tc.src, ToJSON(v), tc.want)
		}
	}
}

func TestFromJSONInvalid(t *testing.T) {
	for _, in := range []string{
		"", " ", "{", "}", "[", "]", `{"a"`, `{"a":`, `{"a":1`, `{,}`, `{"a" 1}`, `[1 2]`,
		"+1", "-", "1e", "1e+", "--1", "-0x10", "nul", "truee", "tru", "True", "TRUE", "NULL", "nan", "+Infinity", "1_2",
		`"abc`, `"\x"`, `"\u12"`, `"\u12g4"`, `"\`, "\x00", "\ufeff1", "@", "new Foo",
	} {
		if v, err := FromJSON([]byte(in)); err == nil {
			t.Errorf("FromJSON(%q) = %v, want error", in, ToJSON(v))
		}
	}
}

func TestFromJSONNumbers(t *testing.T) {
	for in, want := range map[string]float64{
		"1e999": math.Inf(1), "-1e999": math.Inf(-1), "1e-999": 0, "123456789012345678901234567890": 1.2345678901234568e29, "5e-324": 5e-324,
	} {
		v, err := FromJSON([]byte(in))
		if err != nil {
			t.Errorf("FromJSON(%q): %v", in, err)
			continue
		}
		if got := float64(v.(Number)); got != want {
			t.Errorf("FromJSON(%q) = %v, want %v", in, got, want)
		}
	}
	v, _ := FromJSON([]byte("-0"))
	if !math.Signbit(float64(v.(Number))) {
		t.Error("-0 lost its sign")
	}
	if got := ToJSON(v); got != "0" {
		t.Errorf("ToJSON(-0) = %q", got)
	}
}

// TestFromJSONBigIntegers: Json.NET gives integers beyond 64 bits as BigInteger
// and (Double)BigInteger truncates; established with the runner's code
// (fromJSON('N') == N, where the literal N rounds to nearest).
func TestFromJSONBigIntegers(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want float64
	}{
		{"9223372036854775807", 9223372036854775807},   // fits a long: nearest
		{"-9223372036854775808", -9223372036854775808}, // exact
		{"12345678901234567890", 12345678901234567890}, // 64 bits, above long: truncated to the same double here
		{"18446744073709551615", 18446744073709549568}, // 2^64-1 truncates to 2^64-2048, not 2^64
		{"18446744073709551617", 18446744073709551616}, // 2^64+1 truncates to 2^64
		{"-18446744073709551615", -18446744073709549568},
		{"12345678901234567890.0", 12345678901234567168}, // a decimal point: an ordinary float
		{"1e30", 1e30},
	} {
		v, err := FromJSON([]byte(tc.src))
		if err != nil {
			t.Errorf("FromJSON(%q): %v", tc.src, err)
			continue
		}
		if got := float64(v.(Number)); got != tc.want {
			t.Errorf("FromJSON(%q) = %v, want %v", tc.src, got, tc.want)
		}
	}
}

func TestFromJSONInvalidUTF8(t *testing.T) {
	v, err := FromJSON([]byte("\"a\xffb\""))
	if err != nil {
		t.Fatal(err)
	}
	if s := string(v.(String)); s != "a\ufffdb" {
		t.Errorf("got %q", s)
	}
}

func TestFromJSONDeepNesting(t *testing.T) {
	// Json.NET's reader allows 64 levels (JsonReader.MaxDepth).
	for _, n := range []int{1, 63, 64} {
		if _, err := FromJSON([]byte(strings.Repeat("[", n) + "1" + strings.Repeat("]", n))); err != nil {
			t.Errorf("depth %d: %v", n, err)
		}
	}
	for _, open := range []string{"[", `{"a":`} {
		close := map[string]string{"[": "]", `{"a":`: "}"}[open]
		if _, err := FromJSON([]byte(strings.Repeat(open, 65) + "1" + strings.Repeat(close, 65))); err == nil {
			t.Errorf("depth 65 of %q accepted", open)
		}
	}
}

func TestToJSONFormatting(t *testing.T) {
	d := NewDictionary()
	d.Add("name", String("x"))
	d.Add("list", NewArray(Number(1), Null{}, Boolean(true), NewArray(), NewDictionary()))
	inner := NewDictionary()
	inner.Add("k", String("v"))
	d.Add("obj", inner)
	want := `{
  "name": "x",
  "list": [
    1,
    null,
    true,
    [],
    {}
  ],
  "obj": {
    "k": "v"
  }
}`
	if got := ToJSON(d); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	tests := []struct {
		v    Value
		want string
	}{
		{nil, "null"},
		{Null{}, "null"},
		// The runner writes numbers with ConvertToString (G15), so NaN and
		// the infinities are written as such.
		{Number(math.NaN()), "NaN"},
		{Number(math.Inf(1)), "Infinity"},
		{Number(math.Inf(-1)), "-Infinity"},
		{Number(math.Copysign(0, -1)), "0"},
		{Number(1e21), "1E+21"},
		{Number(1e-7), "1E-07"},
		{Number(123456789), "123456789"},
		{String("\u0000\u001f\u007f\u0080\u2028"), `"\u0000\u001f` + "\u007f\u0080" + `\u2028"`},
		{String("\"\\\b\f\n\r\t"), `"\"\\\b\f\n\r\t"`},
		{String("</script>&'"), `"</script>&'"`},
		{String("\xff"), "\"\ufffd\""},
		{(*Array)(nil), "null"},
		{(*Dictionary)(nil), "null"},
		{NewArray(nil), "[\n  null\n]"},
	}
	for _, tc := range tests {
		if got := ToJSON(tc.v); got != tc.want {
			t.Errorf("ToJSON(%#v) = %q, want %q", tc.v, got, tc.want)
		}
	}
}

func TestDictionary(t *testing.T) {
	d := NewDictionary()
	if !d.Add("Name", String("first")) {
		t.Fatal("Add failed")
	}
	if d.Add("NAME", String("second")) {
		t.Error("case variant was added")
	}
	d.Add("other", Number(1))
	if v, ok := d.Get("nAmE"); !ok || v.(String) != "first" {
		t.Errorf("Get = %v, %v", v, ok)
	}
	if _, ok := d.Get("missing"); ok {
		t.Error("Get of missing key succeeded")
	}
	if got := strings.Join(d.Keys(), ","); got != "Name,other" {
		t.Errorf("Keys = %s", got)
	}
	if d.Len() != 2 || len(d.Values()) != 2 || len(d.Pairs()) != 2 || d.Pairs()[0].Key != "Name" {
		t.Errorf("Len/Values/Pairs inconsistent: %d", d.Len())
	}
	// Non-ASCII keys fold like JavaScript's toLowerCase.
	d.Add("ΑΣ", String("sigma"))
	if _, ok := d.Get("ας"); !ok {
		t.Error("final sigma key not found")
	}
	var zero Dictionary
	zero.Add("a", Null{})
	if _, ok := zero.Get("A"); !ok {
		t.Error("zero Dictionary is not usable")
	}
	var nilDict *Dictionary
	if _, ok := nilDict.Get("a"); ok || nilDict.Len() != 0 || nilDict.Keys() != nil || nilDict.Values() != nil || nilDict.Pairs() != nil {
		t.Error("nil Dictionary is not empty")
	}
}

func TestArray(t *testing.T) {
	a := NewArray(String("x"), nil)
	if a.Len() != 2 || a.At(0).(String) != "x" || a.At(1).Kind() != KindNull || a.At(2).Kind() != KindNull || a.At(-1).Kind() != KindNull {
		t.Errorf("At/Len wrong: %v", a)
	}
	a.Append(Number(3))
	if a.Len() != 3 || a.Filtered() {
		t.Error("Append/Filtered")
	}
	items := a.Items()
	items[0] = Null{}
	if a.At(0).Kind() != KindString {
		t.Error("Items does not copy")
	}
	var nilArr *Array
	if nilArr.Len() != 0 || nilArr.Items() != nil || nilArr.At(0).Kind() != KindNull || nilArr.Filtered() {
		t.Error("nil Array is not empty")
	}
	src := []Value{String("a")}
	b := NewArray(src...)
	src[0] = String("changed")
	if b.At(0).(String) != "a" {
		t.Error("NewArray does not copy")
	}
}

func TestCoerceAndTruthy(t *testing.T) {
	for _, tc := range []struct {
		v      Value
		s      string
		truthy bool
	}{
		{nil, "", false},
		{Null{}, "", false},
		{Boolean(true), "true", true},
		{Boolean(false), "false", false},
		{Number(0), "0", false},
		{Number(math.Copysign(0, -1)), "0", false},
		{Number(math.NaN()), "NaN", false},
		{Number(2), "2", true},
		{Number(math.Inf(-1)), "-Infinity", true},
		{String(""), "", false},
		{String("0"), "0", true},
		{String("false"), "false", true},
		{NewArray(), "Array", true},
		{NewDictionary(), "Object", true},
		{(*Array)(nil), "", false},
	} {
		if got := CoerceString(tc.v); got != tc.s {
			t.Errorf("CoerceString(%#v) = %q, want %q", tc.v, got, tc.s)
		}
		if got := Truthy(tc.v); got != tc.truthy {
			t.Errorf("Truthy(%#v) = %v, want %v", tc.v, got, tc.truthy)
		}
	}
	if KindDictionary.String() != "Object" || Kind(99).String() != "unknown" {
		t.Error("Kind.String")
	}
}

func TestToNumber(t *testing.T) {
	for _, tc := range []struct {
		v    Value
		want float64
	}{
		{nil, 0}, {Null{}, 0}, {Boolean(true), 1}, {Boolean(false), 0}, {Number(2.5), 2.5}, {String(" 7 "), 7}, {String("x"), math.NaN()}, {NewArray(), math.NaN()}, {NewDictionary(), math.NaN()},
	} {
		got := toNumber(tc.v)
		if got != tc.want && !(math.IsNaN(got) && math.IsNaN(tc.want)) {
			t.Errorf("toNumber(%#v) = %v, want %v", tc.v, got, tc.want)
		}
	}
}
