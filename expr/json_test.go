// SPDX-License-Identifier: MIT

package expr

import (
	"math"
	"strings"
	"testing"
)

func TestFromJSONValid(t *testing.T) {
	tests := []struct {
		in   string
		want string // ToJSON of the result
	}{
		{"null", "null"},
		{" true ", "true"},
		{"\t\r\n false \n", "false"},
		{"0", "0"},
		{"-0", "0"},
		{"-12.5e1", "-125"},
		{"1E2", "100"},
		{"0.1e1", "1"},
		{`"a\u00e9\n"`, `"aé\n"`},
		{`"\ud83d\ude00"`, `"😀"`},
		{`"\ud800"`, `"\ud800"`},
		{`"\udc00x"`, `"\udc00x"`},
		{`"\ud800\u0041"`, `"\ud800A"`},
		{`"\/"`, `"/"`},
		{`[]`, `[]`},
		{`{}`, `{}`},
		{`[ 1 , [ ] , { } ]`, "[\n  1,\n  [],\n  {}\n]"},
		{`{"a":1,"A":2}`, "{\n  \"a\": 2\n}"}, // see below: the last value of an exact duplicate wins, case variants collapse to the first
		{`{"a":1,"a":2}`, "{\n  \"a\": 2\n}"},
		{`{"b":1,"1":2,"a":3,"0":4}`, "{\n  \"0\": 4,\n  \"1\": 2,\n  \"b\": 1,\n  \"a\": 3\n}"},
	}
	for _, tc := range tests {
		if tc.in == `{"a":1,"A":2}` {
			// "a" and "A" are distinct JSON keys but the same dictionary key:
			// the first one wins.
			tc.want = "{\n  \"a\": 1\n}"
		}
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

func TestFromJSONInvalid(t *testing.T) {
	for _, in := range []string{
		"", " ", "{", "}", "[", "]", `{"a"`, `{"a":`, `{"a":1`, `{"a":1,}`, `{,}`, `{a:1}`, `{'a':1}`, `{"a" 1}`, `[1,]`, `[,1]`, `[1 2]`,
		"01", "1.", ".5", "+1", "-", "1e", "1e+", "--1", "0x10", "NaN", "Infinity", "-Infinity", "undefined", "nul", "truee", "tru", "True",
		`"abc`, `"a` + "\n" + `b"`, "\"\x01\"", `"\x"`, `"\u12"`, `"\u12g4"`, `"\`, "1 2", "[] []", "{} x", `"a""b"`, "\x00", "\u00a01", "1\u00a0", "\ufeff1",
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
	deep := strings.Repeat("[", 900) + strings.Repeat("]", 900)
	if _, err := FromJSON([]byte(deep)); err != nil {
		t.Errorf("depth 900: %v", err)
	}
	tooDeep := strings.Repeat("[", MaxJSONDepth+10) + strings.Repeat("]", MaxJSONDepth+10)
	if _, err := FromJSON([]byte(tooDeep)); err == nil {
		t.Error("absurd depth accepted")
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
		{String("\u0000\u001f\u007f\u0080\u2028"), `"\u0000\u001f` + "\u007f\u0080\u2028" + `"`},
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
