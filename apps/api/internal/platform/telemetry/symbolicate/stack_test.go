package symbolicate

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNeedsList(t *testing.T) {
	got := NeedsList("r8:1.0", "dart:aa", "dart:aa", "engine:bb", "")
	if got != "dart:aa engine:bb r8:1.0" {
		t.Errorf("got %q", got)
	}
	if NeedsList() != "" {
		t.Errorf("empty %q", NeedsList())
	}
}

func TestSynthesizedDartStack(t *testing.T) {
	got := SynthesizeDartStack("00c92cc1f2db62bbc0baf45738c85488", 0x300000, []uint64{0x7ea257, 0xa7854b})
	want := "" +
		"*** *** *** *** *** *** *** *** *** *** *** *** *** *** *** ***\n" +
		"build_id: '00c92cc1f2db62bbc0baf45738c85488'\n" +
		"isolate_dso_base: 0, vm_dso_base: 0\n" +
		"    #00 abs 00000000007ea257 virt 00000000007ea257 _kDartSnapshotText+0x4ea257\n" +
		"    #01 abs 0000000000a7854b virt 0000000000a7854b _kDartSnapshotText+0x77854b\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestLLVMSymbolizerJSON(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "llvm-symbolizer")
	script := "#!/bin/sh\ncat <<'EOF'\n" +
		`[{"Address":"0x1234","ModuleName":"libflutter.so","Symbol":[{"FunctionName":"inner","FileName":"/src/foo.cc","Line":10,"Column":1},{"FunctionName":"outer","FileName":"/src/foo.cc","Line":20,"Column":2}]},{"Address":"0x9999","ModuleName":"libflutter.so","Symbol":[{"FunctionName":"","FileName":"","Line":0,"Column":0}]}]` +
		"\nEOF\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	tools := ExecTools{LLVMSymbolizer: bin}
	addrs, err := tools.Symbolize(context.Background(), filepath.Join(dir, "libflutter.so"), []uint64{0x1234, 0x9999})
	if err != nil {
		t.Fatal(err)
	}
	frames := []Frame{
		{Function: "+0x1234", Module: "libflutter.so", Path: "/data/app/x/libflutter.so", PC: 0x1234, Raw: "orig-inner"},
		{Function: "+0x9999", Module: "libflutter.so", Path: "/data/app/x/libflutter.so", PC: 0x9999, Raw: "orig-unknown"},
	}
	got := ApplyLLVM(frames, addrs)
	if len(got) != 3 {
		t.Fatalf("len=%d %+v", len(got), got)
	}
	if got[0].Function != "inner" || got[0].File != "foo.cc" || got[0].Line != 10 {
		t.Errorf("inner %+v", got[0])
	}
	if got[1].Function != "outer" || got[1].Line != 20 {
		t.Errorf("outer %+v", got[1])
	}
	if got[2].Function != "+0x9999" || got[2].Raw != "orig-unknown" {
		t.Errorf("unknown %+v", got[2])
	}
}
