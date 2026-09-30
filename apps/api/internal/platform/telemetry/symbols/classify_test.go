package symbols

import (
	"strings"
	"testing"
)

func TestClassifyFileName(t *testing.T) {
	cases := []struct {
		name, kind, arch string
		ok               bool
	}{
		{FileARM64Symbols, KindDartSymbols, ArchARM64, true},
		{FileARMSymbols, KindDartSymbols, ArchARM, true},
		{FileX64Symbols, KindDartSymbols, ArchX64, true},
		{FileObfuscation, KindDartObfuscationMap, "", true},
		{FileR8Mapping, KindR8Mapping, "", true},
		{"libflutter.so", "", "", false},
	}
	for _, c := range cases {
		kind, arch, ok := ClassifyFileName(c.name)
		if ok != c.ok || kind != c.kind || arch != c.arch {
			t.Errorf("%s: kind=%q arch=%q ok=%v", c.name, kind, arch, ok)
		}
	}
}

func TestValidateObfuscationMap(t *testing.T) {
	if err := ValidateObfuscationMap(strings.NewReader(`["a","b"]`)); err != nil {
		t.Fatal(err)
	}
	if err := ValidateObfuscationMap(strings.NewReader(`[]`)); err != nil {
		t.Fatal(err)
	}
	if err := ValidateObfuscationMap(strings.NewReader(`["odd"]`)); err == nil {
		t.Fatal("odd length")
	}
	if err := ValidateObfuscationMap(strings.NewReader(`{"a":"b"}`)); err == nil {
		t.Fatal("object")
	}
	if err := ValidateObfuscationMap(strings.NewReader(`[1,2]`)); err == nil {
		t.Fatal("numbers")
	}
}

func TestValidateR8Mapping(t *testing.T) {
	ok := "# comment\nandroidx.Foo -> a.b:\n    void bar() -> a\n"
	if err := ValidateR8Mapping(strings.NewReader(ok)); err != nil {
		t.Fatal(err)
	}
	if err := ValidateR8Mapping(strings.NewReader("nothing here\n")); err == nil {
		t.Fatal("expected error")
	}
}
