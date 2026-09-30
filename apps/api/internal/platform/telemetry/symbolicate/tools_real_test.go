package symbolicate

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func skipEnv(t *testing.T, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if os.Getenv(k) == "" {
			t.Skip(k + " unset")
		}
	}
}

func TestRealDecodeSample(t *testing.T) {
	skipEnv(t, "TELEMETRY_DECODE_BIN", "TELEMETRY_SAMPLE_DIR")
	dir := os.Getenv("TELEMETRY_SAMPLE_DIR")
	stack, err := os.ReadFile(filepath.Join(dir, "stack-exception.txt"))
	if err != nil {
		t.Fatal(err)
	}
	wantRaw, err := os.ReadFile(filepath.Join(dir, "stack-exception.decoded.txt"))
	if err != nil {
		t.Fatal(err)
	}
	tools := ExecTools{DecodeBin: os.Getenv("TELEMETRY_DECODE_BIN")}
	got, err := tools.DecodeDart(context.Background(), filepath.Join(dir, "app.android-arm64.symbols"), string(stack))
	if err != nil {
		t.Fatal(err)
	}
	gotFrames := ParseDecodedDart(got)
	wantFrames := ParseDecodedDart(string(wantRaw))
	if len(gotFrames) == 0 || len(gotFrames) != len(wantFrames) {
		t.Fatalf("frames got=%d want=%d", len(gotFrames), len(wantFrames))
	}
	if gotFrames[0].Function != "_About.build.<anonymous closure>" {
		t.Errorf("fn %q", gotFrames[0].Function)
	}
	if gotFrames[0].File != "package:kungal/features/me/presentation/me_screen.dart" {
		t.Errorf("file %q", gotFrames[0].File)
	}
	for i := range wantFrames {
		if gotFrames[i].Function != wantFrames[i].Function || gotFrames[i].File != wantFrames[i].File {
			t.Errorf("frame %d got %q %q want %q %q", i, gotFrames[i].Function, gotFrames[i].File, wantFrames[i].Function, wantFrames[i].File)
		}
	}
}

func TestRealNativeLibappFrame(t *testing.T) {
	skipEnv(t, "TELEMETRY_DECODE_BIN", "TELEMETRY_SAMPLE_DIR")
	dir := os.Getenv("TELEMETRY_SAMPLE_DIR")
	f, err := os.Open(filepath.Join(dir, "app.android-arm64.symbols"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	textStart, err := TextStart(f)
	if err != nil {
		t.Fatal(err)
	}
	const buildID = "00c92cc1f2db62bbc0baf45738c85488"
	const pc = uint64(0x7ea257)
	synth := SynthesizeDartStack(buildID, textStart, []uint64{pc})
	tools := ExecTools{DecodeBin: os.Getenv("TELEMETRY_DECODE_BIN")}
	got, err := tools.DecodeDart(context.Background(), filepath.Join(dir, "app.android-arm64.symbols"), synth)
	if err != nil {
		t.Fatal(err)
	}
	frames := ParseDecodedDart(got)
	if len(frames) == 0 || frames[0].Function != "_About.build.<anonymous closure>" {
		t.Fatalf("frames %+v out %q", frames, got)
	}
	raw := `#00 pc 00000000007ea257  /data/app/x/lib/arm64/libapp.so (BuildId: 00c92cc1f2db62bbc0baf45738c85488)`
	parsed := ParseDebuggerd(raw)
	if len(parsed) != 1 || parsed[0].PC != pc || parsed[0].Module != "libapp.so" {
		t.Fatalf("debuggerd %+v", parsed)
	}
}

func TestTextStartFromSample(t *testing.T) {
	skipEnv(t, "TELEMETRY_SAMPLE_DIR")
	f, err := os.Open(filepath.Join(os.Getenv("TELEMETRY_SAMPLE_DIR"), "app.android-arm64.symbols"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := TextStart(f)
	if err != nil {
		t.Fatal(err)
	}
	if got != 0x300000 {
		t.Errorf("textStart=0x%x want 0x300000", got)
	}
}

func TestRealRetrace(t *testing.T) {
	skipEnv(t, "TELEMETRY_JAVA_BIN", "TELEMETRY_R8_JAR", "TELEMETRY_SAMPLE_DIR")
	mapping := filepath.Join(os.Getenv("TELEMETRY_SAMPLE_DIR"), "mapping.txt")
	stack := "java.lang.IllegalStateException: boom\n\tat d70.f(SourceFile:5)\n\tat d70.a(SourceFile:3)\n"
	tools := ExecTools{JavaBin: os.Getenv("TELEMETRY_JAVA_BIN"), R8Jar: os.Getenv("TELEMETRY_R8_JAR")}
	got, err := tools.Retrace(context.Background(), mapping, stack)
	if err != nil {
		t.Fatal(err)
	}
	frames := ParseJava(got)
	if len(frames) < 3 {
		t.Fatalf("frames=%d out=%q", len(frames), got)
	}
	want := []string{
		"io.flutter.embedding.engine.plugins.FlutterPlugin$FlutterPluginBinding.getApplicationContext",
		"dev.nextmoe.telemetry.TelemetryPlugin.onAttachedToEngine",
		"dev.nextmoe.telemetry.TelemetryPlugin.getActivities",
	}
	for i, fn := range want {
		if frames[i].Function != fn {
			t.Errorf("frame %d %q want %q", i, frames[i].Function, fn)
		}
	}
}

func TestRealDeobfuscationMap(t *testing.T) {
	skipEnv(t, "TELEMETRY_SAMPLE_DIR")
	raw, err := os.ReadFile(filepath.Join(os.Getenv("TELEMETRY_SAMPLE_DIR"), "obfuscation.map.json"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := ParseObfuscationMap(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(m) == 0 {
		t.Fatal("empty map")
	}
	for obf, want := range map[string]string{
		"Obc":  "ListUserWallCommentsResponse401",
		"_UFa": "_KunChatQuickReaction",
	} {
		if got := DeobfuscateType(obf, m); got != want {
			t.Errorf("DeobfuscateType(%q) = %q, want %q", obf, got, want)
		}
	}
}
