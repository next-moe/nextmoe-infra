package symbolicate

import (
	"strings"
	"testing"
)

func TestNormaliseDartPath(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{
			"/home/runner/work/x/apps/kungal/lib/features/me/presentation/me_screen.dart",
			"package:kungal/features/me/presentation/me_screen.dart",
		},
		{
			"/home/kun/fvm/versions/3.47.4/packages/flutter/lib/src/gestures/recognizer.dart",
			"package:flutter/src/gestures/recognizer.dart",
		},
		{
			"/home/user/.pub-cache/hosted/pub.dev/riverpod-3.1.0/lib/src/x.dart",
			"package:riverpod/src/x.dart",
		},
		{"package:kungal/foo.dart", "package:kungal/foo.dart"},
		{"dart:core", "dart:core"},
		{"file:///home/runner/work/x/apps/kungal/lib/foo.dart", "package:kungal/foo.dart"},
		{"/tmp/scratch/recognizer.dart", "recognizer.dart"},
	}
	for _, c := range cases {
		if got := NormaliseDartPath(c.in); got != c.want {
			t.Errorf("NormaliseDartPath(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestParseDecodedDart(t *testing.T) {
	in := strings.Join([]string{
		"*** *** *** *** *** *** *** *** *** *** *** *** *** *** *** ***",
		"build_id: '00c92cc1f2db62bbc0baf45738c85488'",
		"#0      _About.build.<anonymous closure> (/home/kun/Desktop/code/website/kungal-apps/apps/kungal/lib/features/me/presentation/me_screen.dart:301:15)",
		"<asynchronous suspension>",
		"#1      GestureRecognizer.invokeCallback (/home/kun/fvm/versions/3.47.4/packages/flutter/lib/src/gestures/recognizer.dart:362:24)",
	}, "\n")
	got := ParseDecodedDart(in)
	if len(got) != 2 {
		t.Fatalf("len=%d want 2: %+v", len(got), got)
	}
	if got[0].Function != "_About.build.<anonymous closure>" {
		t.Errorf("fn0=%q", got[0].Function)
	}
	if got[0].File != "package:kungal/features/me/presentation/me_screen.dart" {
		t.Errorf("file0=%q", got[0].File)
	}
	if got[0].Line != 301 {
		t.Errorf("line0=%d", got[0].Line)
	}
	if got[1].File != "package:flutter/src/gestures/recognizer.dart" {
		t.Errorf("file1=%q", got[1].File)
	}
}

func TestParseUndecodedDart(t *testing.T) {
	in := `
*** *** *** *** *** *** *** *** *** *** *** *** *** *** *** ***
build_id: '00c92cc1f2db62bbc0baf45738c85488'
    #00 abs 0000007b2941d257 virt 00000000007ea257 _kDartSnapshotText+0x4ea257
    #01 abs 0000007b296ab54b virt 0000000000a7854b _kDartSnapshotText+0x77854b
`
	got := ParseUndecodedDart(in)
	if len(got) != 2 {
		t.Fatalf("len=%d %+v", len(got), got)
	}
	if got[0].Function != "_kDartSnapshotText+0x4ea257" || got[0].File != "" {
		t.Errorf("%+v", got[0])
	}
	if got[1].Function != "_kDartSnapshotText+0x77854b" {
		t.Errorf("%+v", got[1])
	}
}

func TestParseJava(t *testing.T) {
	in := `java.lang.IllegalStateException: boom
	at d70.f(SourceFile:5)
	at d70.a(SourceFile:3)
Caused by: java.lang.RuntimeException: x
	at other.z(Other.kt:1)
`
	got := ParseJava(in)
	if len(got) != 2 {
		t.Fatalf("len=%d %+v", len(got), got)
	}
	if got[0].Function != "d70.f" || got[0].File != "SourceFile" || got[0].Line != 5 {
		t.Errorf("%+v", got[0])
	}
	if got[1].Function != "d70.a" {
		t.Errorf("%+v", got[1])
	}
}

func TestParseDebuggerd(t *testing.T) {
	in := strings.Join([]string{
		`#00 pc 00000000000e714c  /apex/com.android.runtime/lib64/bionic/libc.so (__epoll_pwait+12) (BuildId: bff603430d1ad139d4f0e6a78f542c4d)`,
		`#01 pc 00000000007ea257  /data/app/x/lib/arm64/libapp.so (BuildId: 00c92cc1f2db62bbc0baf45738c85488)`,
		`#02 pc 0000000000001234  /data/app/x/lib/arm64/libflutter.so`,
		`#03 pc 000000000000abcd  [anon_shmem:dalvik-jit-code-cache]`,
	}, "\n")
	got := ParseDebuggerd(in)
	if len(got) != 4 {
		t.Fatalf("len=%d %+v", len(got), got)
	}
	if got[0].Function != "__epoll_pwait" || got[0].Module != "libc.so" || got[0].BuildID != "bff603430d1ad139d4f0e6a78f542c4d" {
		t.Errorf("0 %+v", got[0])
	}
	if got[0].Path != "/apex/com.android.runtime/lib64/bionic/libc.so" {
		t.Errorf("path0 %q", got[0].Path)
	}
	if got[1].Function != "+0x7ea257" || got[1].Module != "libapp.so" || got[1].BuildID == "" {
		t.Errorf("1 %+v", got[1])
	}
	if got[2].Function != "+0x1234" || got[2].BuildID != "" {
		t.Errorf("2 %+v", got[2])
	}
	if got[3].Module != "[anon_shmem:dalvik-jit-code-cache]" || got[3].Path != "[anon_shmem:dalvik-jit-code-cache]" {
		t.Errorf("3 %+v", got[3])
	}
}

func TestMainThreadSection(t *testing.T) {
	in := `"FinalizerDaemon" prio=5 tid=3 Native
  at java.lang.Object.wait(Native Method)

"main" prio=5 tid=1 Native
  at android.os.MessageQueue.nativePollOnce(Native Method)
  at android.os.MessageQueue.next(MessageQueue.java:335)

"Binder" prio=5 tid=2 Native
  at other.skip(Skip.java:1)
`
	sec := MainThreadSection(in)
	got := ParseJava(sec)
	if len(got) != 2 {
		t.Fatalf("len=%d sec=%q got=%+v", len(got), sec, got)
	}
	if got[0].Function != "android.os.MessageQueue.nativePollOnce" {
		t.Errorf("%+v", got[0])
	}
	if got[1].Function != "android.os.MessageQueue.next" || got[1].File != "MessageQueue.java" {
		t.Errorf("%+v", got[1])
	}
}
