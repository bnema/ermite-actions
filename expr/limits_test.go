// SPDX-License-Identifier: MIT

package expr

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode"
)

func mustParse(t testing.TB, src string, ctxNames []string, funcs []FuncInfo) Node {
	t.Helper()
	n, err := Parse(src, ctxNames, funcs)
	if err != nil {
		t.Fatalf("Parse(%.60q): %v", src, err)
	}
	return n
}

func evalWith(t testing.TB, src string, ec EvalContext) (Value, error) {
	t.Helper()
	return Evaluate(context.Background(), mustParse(t, src, []string{"big", "github"}, nil), ec)
}

func asEvalError(t testing.TB, err error) *EvalError {
	t.Helper()
	var ee *EvalError
	if !errors.As(err, &ee) {
		t.Fatalf("error %v (%T) is not an *EvalError", err, err)
	}
	return ee
}

// An expression of a few hundred bytes that would build a gigabyte string
// without a budget: each level of format doubles the text.
func doublingExpr(levels int) string {
	e := "'x'"
	for i := 0; i < levels; i++ {
		e = "format('{0}{0}', " + e + ")"
	}
	return e
}

func TestOutputBudgetFormat(t *testing.T) {
	// The doubling chain charges every intermediate string: 19 levels cost
	// 2^20-2 bytes in total, which just fits the default budget of 1 MiB;
	// 20 levels do not.
	if _, err := evalWith(t, doublingExpr(19), EvalContext{}); err != nil {
		t.Fatalf("19 doublings: %v", err)
	}
	_, err := evalWith(t, doublingExpr(20), EvalContext{})
	if err == nil {
		t.Fatal("a chain of 20 doublings fits the budget?")
	}
	if ee := asEvalError(t, err); !strings.Contains(ee.Message, "exceeds the limit of 1048576 bytes") {
		t.Errorf("message = %q", ee.Message)
	}
	if v, err := evalWith(t, doublingExpr(10), EvalContext{}); err != nil || len(CoerceString(v)) != 1024 {
		t.Errorf("10 doublings: %v, %v", v, err)
	}
	// Absurd depth is cut off long before memory is.
	if _, err := evalWith(t, doublingExpr(40), EvalContext{}); err == nil {
		t.Error("40 doublings succeeded")
	}
	// The limit can be raised, lowered or removed.
	if _, err := evalWith(t, doublingExpr(12), EvalContext{MaxResultBytes: 4096}); err == nil {
		t.Error("small limit not applied")
	}
	if v, err := evalWith(t, doublingExpr(20), EvalContext{MaxResultBytes: -1}); err != nil || len(CoerceString(v)) != 1<<20 {
		t.Errorf("unlimited: %v", err)
	}
	if v, err := evalWith(t, doublingExpr(12), EvalContext{MaxResultBytes: 1 << 20}); err != nil || len(CoerceString(v)) != 4096 {
		t.Errorf("explicit limit: %v", err)
	}
	// A single oversized argument.
	big := String(strings.Repeat("y", 100))
	_, err = evalWith(t, "format('{0}{0}', big)", EvalContext{Contexts: map[string]Value{"big": big}, MaxResultBytes: 150})
	if err == nil {
		t.Error("format over the limit succeeded")
	}
	if _, err = evalWith(t, "format('{0}{0}', big)", EvalContext{Contexts: map[string]Value{"big": big}, MaxResultBytes: 200}); err != nil {
		t.Errorf("format at the limit failed: %v", err)
	}
}

func bigArray(n int) *Array {
	a := NewArray()
	for i := 0; i < n; i++ {
		a.Append(String("item"))
	}
	return a
}

func TestOutputBudgetJoin(t *testing.T) {
	ec := EvalContext{Contexts: map[string]Value{"big": bigArray(1000)}}
	// 1000 items of 4 bytes and 999 separators of 2: about 6 KB.
	if _, err := evalWith(t, "join(big, ', ')", ec); err != nil {
		t.Fatal(err)
	}
	ec.MaxResultBytes = 5000
	if _, err := evalWith(t, "join(big, ', ')", ec); err == nil {
		t.Error("join over the limit succeeded")
	}
	// A huge separator is charged too.
	ec = EvalContext{Contexts: map[string]Value{"big": bigArray(1000), "github": String(strings.Repeat("s", 2000))}, MaxResultBytes: 1 << 20}
	if _, err := evalWith(t, "join(big, github)", ec); err == nil { // 999 * 2000 bytes of separators
		t.Error("a 2 MB join fits a 1 MiB budget?")
	}
}

func TestOutputBudgetToJSON(t *testing.T) {
	ec := EvalContext{Contexts: map[string]Value{"big": bigArray(1000)}}
	if _, err := evalWith(t, "toJSON(big)", ec); err != nil {
		t.Fatal(err)
	}
	ec.MaxResultBytes = 1000
	_, err := evalWith(t, "toJSON(big)", ec)
	if err == nil {
		t.Fatal("toJSON over the limit succeeded")
	}
	if ee := asEvalError(t, err); !strings.Contains(ee.Message, "exceeds the limit of 1000 bytes") {
		t.Errorf("message = %q", ee.Message)
	}
	// The budget is shared: two toJSON of 400 bytes each exceed 700.
	ec.Contexts = map[string]Value{"big": bigArray(40)}
	ec.MaxResultBytes = 700
	if _, err := evalWith(t, "toJSON(big)", ec); err != nil {
		t.Fatalf("one toJSON: %v", err)
	}
	if _, err := evalWith(t, "toJSON(big) && toJSON(big)", ec); err == nil {
		t.Error("budget is not shared between calls")
	}
}

func TestOutputBudgetFilter(t *testing.T) {
	// A filter over 100 arrays of 100 items, then a second flattening: the
	// elements built are charged.
	outer := NewArray()
	for i := 0; i < 100; i++ {
		outer.Append(bigArray(100))
	}
	ec := EvalContext{Contexts: map[string]Value{"big": outer}}
	if _, err := evalWith(t, "big.*.*", ec); err != nil {
		t.Fatalf("filter within budget: %v", err)
	}
	ec.MaxResultBytes = 100 * 16 // the first .* costs 1600, the second 160000
	if _, err := evalWith(t, "big.*.*", ec); err == nil {
		t.Error("filter over the budget succeeded")
	}
	// A fan-out through shared arrays: an array of 1000 references to one
	// array of 1000 items yields a million elements after two stars.
	shared := bigArray(1000)
	fan := NewArray()
	for i := 0; i < 1000; i++ {
		fan.Append(shared)
	}
	ec = EvalContext{Contexts: map[string]Value{"big": fan}}
	_, err := evalWith(t, "big.*.*.*", ec)
	if err == nil {
		t.Fatal("fan-out filter succeeded")
	}
	_ = asEvalError(t, err)
	// Lookups that do not build arrays are free.
	if _, err := evalWith(t, "big[0][0]", EvalContext{Contexts: map[string]Value{"big": fan}, MaxResultBytes: 1}); err != nil {
		t.Errorf("indexing charged the budget: %v", err)
	}
}

func TestEvaluateContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	n := mustParse(t, "1 == 1", nil, nil)
	_, err := Evaluate(ctx, n, EvalContext{})
	ee := asEvalError(t, err)
	if !errors.Is(err, context.Canceled) || ee.Unwrap() != context.Canceled {
		t.Errorf("err = %v, want context.Canceled", err)
	}

	// Cancelled from inside a function while a big filter and contains loop run.
	arr := bigArray(1 << 16)
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	ec := EvalContext{
		Contexts: map[string]Value{"big": arr, "github": String("g")},
		Funcs: map[string]Func{"stop": func(context.Context, []Value) (Value, error) {
			cancel()
			return Null{}, nil
		}},
		MaxResultBytes: -1,
	}
	node := mustParse(t, "stop() || contains(big, 'nope') || join(big, ',')", []string{"big", "github"}, []FuncInfo{{Name: "stop"}})
	_, err = Evaluate(ctx, node, ec)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("cancel inside evaluation: err = %v", err)
	}

	// A deadline during a long loop.
	ctx, cancel2 := context.WithTimeout(context.Background(), 0)
	defer cancel2()
	<-ctx.Done()
	if _, err = Evaluate(ctx, mustParse(t, "contains(big, 'nope')", []string{"big"}, nil), EvalContext{Contexts: map[string]Value{"big": arr}}); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("deadline: err = %v", err)
	}
	// A nil context behaves as Background.
	//nolint:staticcheck // deliberately passing nil
	if v, err := Evaluate(nil, n, EvalContext{}); err != nil || v != Boolean(true) {
		t.Errorf("nil context: %v, %v", v, err)
	}
}

func TestFuncReceivesContext(t *testing.T) {
	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "marker")
	var got any
	ec := EvalContext{Funcs: map[string]Func{"probe": func(c context.Context, _ []Value) (Value, error) {
		got = c.Value(key{})
		return Null{}, nil
	}}}
	if _, err := Evaluate(ctx, mustParse(t, "probe()", nil, []FuncInfo{{Name: "probe"}}), ec); err != nil {
		t.Fatal(err)
	}
	if got != "marker" {
		t.Errorf("function did not receive the evaluation context: %v", got)
	}
}

func TestHandBuiltASTArity(t *testing.T) {
	call := func(name string, n int) Node {
		args := make([]Node, n)
		for i := range args {
			args[i] = &Literal{Value: String("x")}
		}
		return &FunctionCall{Name: Token{Type: TokenIdentifier, Lexeme: name}, Args: args}
	}
	for _, tc := range []struct {
		name string
		n    int
		want string
	}{
		{"contains", 0, "Too few parameters supplied: 'contains'"},
		{"contains", 1, "Too few parameters supplied: 'contains'"},
		{"contains", 3, "Too many parameters supplied: 'contains'"},
		{"startsWith", 1, "Too few parameters supplied: 'startsWith'"},
		{"endsWith", 0, "Too few parameters supplied: 'endsWith'"},
		{"join", 0, "Too few parameters supplied: 'join'"},
		{"join", 3, "Too many parameters supplied: 'join'"},
		{"toJSON", 0, "Too few parameters supplied: 'toJSON'"},
		{"fromJSON", 2, "Too many parameters supplied: 'fromJSON'"},
		{"format", 0, "Too few parameters supplied: 'format'"},
		{"format", 256, "Too many parameters supplied: 'format'"},
		{"case", 0, "Too few parameters supplied: 'case'"},
		{"case", 2, "Too few parameters supplied: 'case'"},
		{"case", 4, "Even number of parameters supplied, requires an odd number of parameters: 'case'"},
		{"nosuch", 1, "function nosuch is not implemented"},
	} {
		var err error
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s(%d args) panicked: %v", tc.name, tc.n, r)
				}
			}()
			_, err = Evaluate(context.Background(), call(tc.name, tc.n), EvalContext{})
		}()
		if ee := (&EvalError{}); !errors.As(err, &ee) || ee.Message != tc.want {
			t.Errorf("%s(%d args): err = %v, want %q", tc.name, tc.n, err, tc.want)
		}
	}
	// Other malformed trees must not panic either.
	for _, n := range []Node{&Star{}, &Logical{}, &Binary{Left: &Literal{}, Right: &Literal{}}, &IndexAccess{Expr: &Literal{}, Index: &Literal{}}, &Literal{Value: nil}, &Unary{Expr: &Literal{}}} {
		if _, err := Evaluate(context.Background(), n, EvalContext{}); err != nil {
			_ = asEvalError(t, err)
		}
	}
}

func TestEvalDepthCap(t *testing.T) {
	var n Node = &Literal{Value: Boolean(true)}
	for i := 0; i < maxEvalDepth+10; i++ {
		n = &Grouping{Group: n}
	}
	_, err := Evaluate(context.Background(), n, EvalContext{})
	if ee := asEvalError(t, err); !strings.Contains(ee.Message, "deeper than") {
		t.Errorf("message = %q", ee.Message)
	}
}

func TestExtensionErrorsAreWrapped(t *testing.T) {
	sentinel := errors.New("kaput")
	ec := EvalContext{Funcs: map[string]Func{
		"boom":    func(context.Context, []Value) (Value, error) { return nil, sentinel },
		"wrapped": func(context.Context, []Value) (Value, error) { return nil, fmt.Errorf("while hashing: %w", sentinel) },
		"already": func(context.Context, []Value) (Value, error) { return nil, &EvalError{Message: "mine"} },
	}}
	infos := []FuncInfo{{Name: "boom"}, {Name: "wrapped"}, {Name: "already"}}
	for src, msg := range map[string]string{"boom()": "kaput", "wrapped()": "while hashing: kaput", "already()": "mine"} {
		_, err := Evaluate(context.Background(), mustParse(t, src, nil, infos), ec)
		ee := asEvalError(t, err)
		if ee.Message != msg {
			t.Errorf("%s: message %q, want %q", src, ee.Message, msg)
		}
		if src != "already()" && (!errors.Is(err, sentinel) || ee.Unwrap() == nil) {
			t.Errorf("%s: sentinel not reachable with errors.Is", src)
		}
	}
}

func cyclic() *Array {
	a := NewArray()
	a.Append(a)
	return a
}

func TestToJSONCycle(t *testing.T) {
	a := cyclic()
	if got := ToJSON(a); got != "null" {
		t.Errorf("ToJSON(cycle) = %.40q, want null", got)
	}
	if _, err := EncodeJSON(a, -1); err == nil || !strings.Contains(err.Error(), "nested too deeply") {
		t.Errorf("EncodeJSON(cycle) err = %v", err)
	}
	d := NewDictionary()
	d.Add("self", d)
	if _, err := EncodeJSON(d, -1); err == nil {
		t.Error("dictionary cycle encoded")
	}
	// Evaluated: toJSON(cycle) is an *EvalError, not a stack overflow.
	_, err := evalWith(t, "toJSON(big)", EvalContext{Contexts: map[string]Value{"big": a}})
	_ = asEvalError(t, err)
	// Everything within MaxJSONDepth still works, and the cut is exact.
	deep := Value(Number(1))
	for i := 0; i < MaxJSONDepth; i++ {
		deep = NewArray(deep)
	}
	if _, err := EncodeJSON(deep, -1); err != nil {
		t.Errorf("depth %d: %v", MaxJSONDepth, err)
	}
	if _, err := EncodeJSON(NewArray(deep), -1); err == nil {
		t.Errorf("depth %d accepted", MaxJSONDepth+1)
	}
}

func TestEncodeJSONLimit(t *testing.T) {
	a := bigArray(100)
	full, err := EncodeJSON(a, -1)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := EncodeJSON(a, len(full)); err != nil || got != full {
		t.Errorf("exact limit: %v", err)
	}
	if _, err := EncodeJSON(a, len(full)-1); err == nil {
		t.Error("one byte over the limit accepted")
	}
	if _, err := EncodeJSON(String(strings.Repeat("a", 100)), 50); err == nil {
		t.Error("long string over the limit accepted")
	}
	if got, err := EncodeJSON(Null{}, 0); err == nil {
		t.Errorf("limit 0: got %q", got)
	}
}

func TestOverLongInputIsRejectedCheaply(t *testing.T) {
	huge := strings.Repeat("a", 64<<20)
	allocs := testing.AllocsPerRun(3, func() {
		if _, err := Lex(huge); err == nil {
			t.Fatal("64 MiB expression accepted")
		}
	})
	if allocs > 3 {
		t.Errorf("rejecting a 64 MiB expression allocated %v times", allocs)
	}
	if exceedsUTF16Len("😀", 1) != true || exceedsUTF16Len("😀", 2) || exceedsUTF16Len("ab", 2) || !exceedsUTF16Len("abc", 2) || exceedsUTF16Len("", 0) {
		t.Error("exceedsUTF16Len")
	}
	if exceedsUTF16Len("\xff\xff", 2) || !exceedsUTF16Len("\xff\xff\xff", 2) {
		t.Error("invalid bytes count as one unit each")
	}
}

// The case tables are generated relative to the Unicode version of the Go
// toolchain: another toolchain needs them regenerated.
func TestCasingTablesMatchGoUnicodeVersion(t *testing.T) {
	if unicode.Version != casingGoUnicodeVersion {
		t.Errorf("casing_tables.go was generated with Go's Unicode %s but this toolchain has %s: regenerate with `go generate ./expr` (needs node) and commit the result",
			casingGoUnicodeVersion, unicode.Version)
	}
}

func TestWTF8Helpers(t *testing.T) {
	hi := "\xed\xa0\x80" // U+D800
	lo := "\xed\xb0\x80" // U+DC00
	if !hasWTF8(hi) || hasWTF8("abc") || hasWTF8("é\xed") {
		t.Error("hasWTF8")
	}
	if r, w := decodeRune(hi); r != 0xD800 || w != 3 {
		t.Errorf("decodeRune = %U, %d", r, w)
	}
	for _, f := range []func(string) string{jsToUpper, toUpperSpecial, jsToLower, normalizeWTF8} {
		if got := f("a" + hi + "b"); !strings.Contains(got, hi) || strings.Contains(got, "\ufffd") {
			t.Errorf("lone surrogate not preserved: %q", got)
		}
	}
	if got := normalizeWTF8("\xed\xa0\xbd\xed\xb8\x80"); got != "😀" {
		t.Errorf("surrogate pair not joined: %q", got)
	}
	if got := normalizeWTF8(lo + hi); got != lo+hi {
		t.Errorf("reversed pair joined: %q", got)
	}
	if foldKey(hi) != hi || foldKey("a"+hi) != "A"+hi {
		t.Error("foldKey")
	}
	if compareUTF16(hi, "\ue000") >= 0 || compareUTF16(lo, "\ue000") >= 0 || compareUTF16(hi, "\U00010000") >= 0 {
		t.Error("lone surrogates sort before U+E000 and before supplementary characters starting with the same unit")
	}
	if !jsIncludes("\U0001F600", "\xed\xa0\xbd") || !jsHasPrefix("\U0001F600", "\xed\xa0\xbd") || !jsHasSuffix("\U0001F600", "\xed\xb8\x80") {
		t.Error("search by lone surrogate inside a supplementary character")
	}
}

func TestLoneSurrogateExpressions(t *testing.T) {
	// The cases the review asked for; the oracle covers many more.
	runEvalCases(t, []evalCase{
		{`fromJSON('"\ud800"') == fromJSON('"\udc00"')`, "Boolean:false"},
		{`fromJSON('"\ud800"') == fromJSON('"\ud800"')`, "Boolean:true"},
		{"fromJSON('\"\\ud800\"') == '\ufffd'", "Boolean:false"},
		{`fromJSON('"\ud800"') < fromJSON('"\udc00"')`, "Boolean:true"},
		{"fromJSON('\"\\ud800\"') < '\ue000'", "Boolean:true"},
		{"fromJSON('\"\\ud800\"') < '\U00010000'", "Boolean:true"},
		{`contains(fromJSON('"a\ud800"'), fromJSON('"\ud800"'))`, "Boolean:true"},
		{`contains(fromJSON('"\ud83d\ude00"'), fromJSON('"\ud83d"'))`, "Boolean:true"},
		{`format('{0}{1}', fromJSON('"\ud83d"'), fromJSON('"\ude00"')) == '😀'`, "Boolean:true"},
		{`toJSON(fromJSON('"\ud800"'))`, `String:"\ud800"`},
	})
}
