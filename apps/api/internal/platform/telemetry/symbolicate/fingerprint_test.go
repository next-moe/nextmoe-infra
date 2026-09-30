package symbolicate

import "testing"

func TestInAppSelection(t *testing.T) {
	prefixes := []string{"package:kungal/", "package:nextmoe_"}
	frames := []Frame{
		{Function: "core", File: "dart:core"},
		{Function: "flutter", File: "package:flutter/src/x.dart"},
		{Function: "appFn", File: "package:kungal/features/a.dart"},
		{Function: "appFn2", File: "package:kungal/features/b.dart"},
	}
	got := SelectFrames(KindException, frames, prefixes)
	if len(got) != 2 || got[0].Function != "appFn" || got[1].Function != "appFn2" {
		t.Fatalf("in-app %+v", got)
	}

	noApp := []Frame{
		{Function: "core", File: "dart:core"},
		{Function: "flutter", File: "package:flutter/src/x.dart"},
		{Function: "riverpod", File: "package:riverpod/src/x.dart"},
		{Function: "other", File: "package:other/y.dart"},
	}
	got = SelectFrames(KindException, noApp, prefixes)
	if len(got) != 2 || got[0].Function != "riverpod" || got[1].Function != "other" {
		t.Fatalf("non-system %+v", got)
	}

	onlySys := []Frame{
		{Function: "core", File: "dart:core"},
		{Function: "flutter", File: "package:flutter/src/x.dart"},
	}
	got = SelectFrames(KindException, onlySys, prefixes)
	if len(got) != 2 || got[0].Function != "core" {
		t.Fatalf("any %+v", got)
	}
}

func TestFingerprintStable(t *testing.T) {
	a := []Frame{
		{Function: "appFn", File: "package:kungal/a.dart", Line: 10, Raw: "abs 0001"},
		{Function: "appFn2", File: "package:kungal/b.dart", Line: 20, Raw: "abs 0002"},
	}
	b := []Frame{
		{Function: "appFn", File: "package:kungal/a.dart", Line: 99, Raw: "abs ffff"},
		{Function: "appFn2", File: "package:kungal/b.dart", Line: 88, Raw: "abs eeee"},
	}
	prefixes := []string{"package:kungal/"}
	fa := Fingerprint(FingerprintInput{Kind: KindException, ExceptionType: "StateError", Frames: SelectFrames(KindException, a, prefixes)})
	fb := Fingerprint(FingerprintInput{Kind: KindException, ExceptionType: "StateError", Frames: SelectFrames(KindException, b, prefixes)})
	if fa != fb {
		t.Errorf("unstable %s vs %s", fa, fb)
	}
	c := []Frame{
		{Function: "otherFn", File: "package:kungal/a.dart", Line: 10},
		{Function: "appFn2", File: "package:kungal/b.dart", Line: 20},
	}
	fc := Fingerprint(FingerprintInput{Kind: KindException, ExceptionType: "StateError", Frames: SelectFrames(KindException, c, prefixes)})
	if fc == fa {
		t.Error("different function should change fingerprint")
	}
}

func TestFingerprintPerKind(t *testing.T) {
	frames := []Frame{{Function: "appFn", File: "package:kungal/a.dart", Module: ""}}
	java := []Frame{{Function: "dev.nextmoe.Foo.bar", File: "Foo.kt"}}
	native := []Frame{{Function: "foo", Module: "libapp.so", Path: "/data/app/x/libapp.so"}}
	seen := map[string]string{}
	check := func(name string, in FingerprintInput) {
		t.Helper()
		fp := Fingerprint(in)
		if fp == "" || len(fp) != 32 {
			t.Errorf("%s fp %q", name, fp)
		}
		if other, ok := seen[fp]; ok {
			t.Errorf("%s collided with %s", name, other)
		}
		seen[fp] = name
	}
	check("exception", FingerprintInput{Kind: KindException, ExceptionType: "StateError", Frames: frames})
	check("contract", FingerprintInput{Kind: KindContract, ExceptionType: "StateError", Message: "expected String", Frames: frames})
	check("server", FingerprintInput{Kind: KindServer, Method: "GET", Path: "/v2/x/:id", Status: "500"})
	check("java", FingerprintInput{Kind: KindJava, ExceptionType: "java.lang.IllegalStateException", Frames: java})
	check("anr", FingerprintInput{Kind: KindANR, Frames: java})
	check("native", FingerprintInput{Kind: KindNative, ExceptionType: "SIGSEGV (SI_USER)", Frames: native})
}

func TestTitle(t *testing.T) {
	if g := Title(FingerprintInput{Kind: KindException, ExceptionType: "StateError", Message: "bad\nmore"}, ""); g != "StateError: bad" {
		t.Errorf("exception %q", g)
	}
	if g := Title(FingerprintInput{Kind: KindException, ExceptionType: "StateError"}, ""); g != "StateError" {
		t.Errorf("type only %q", g)
	}
	if g := Title(FingerprintInput{Kind: KindServer, Method: "GET", Path: "/v2/x/:id", Status: "500"}, ""); g != "GET /v2/x/:id → 500" {
		t.Errorf("server %q", g)
	}
	if g := Title(FingerprintInput{Kind: KindANR}, "android.os.MessageQueue.next"); g != "ANR android.os.MessageQueue.next" {
		t.Errorf("anr %q", g)
	}
	long := Title(FingerprintInput{Kind: KindException, ExceptionType: "E", Message: string(make([]byte, 300))}, "")
	if len([]rune(long)) != 200 {
		t.Errorf("truncate %d", len([]rune(long)))
	}
}
