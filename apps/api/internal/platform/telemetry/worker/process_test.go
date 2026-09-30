package worker

import (
	"context"
	"errors"
	"testing"

	"api/internal/platform/telemetry/model"
	"api/internal/platform/telemetry/store"
	"api/internal/platform/telemetry/symbolicate"
)

type fakeTools struct {
	decodeOut, retraceOut         string
	decodeErr, retraceErr, symErr error
	addrs                         []symbolicate.LLVMAddress
}

func (f fakeTools) DecodeDart(context.Context, string, string) (string, error) {
	return f.decodeOut, f.decodeErr
}
func (f fakeTools) Retrace(context.Context, string, string) (string, error) {
	return f.retraceOut, f.retraceErr
}
func (f fakeTools) Symbolize(context.Context, string, []uint64) ([]symbolicate.LLVMAddress, error) {
	return f.addrs, f.symErr
}

type fakeLookup struct {
	dart   map[string]*model.SymbolFile
	r8     map[string]*model.SymbolFile
	engine map[string]*model.EngineSymbol
	maps   map[int64]*model.SymbolFile
	files  map[string]string
	text   uint64
}

func (f fakeLookup) DartSymbolsByBuildID(_ context.Context, _ int64, id string) (*model.SymbolFile, error) {
	if x, ok := f.dart[id]; ok {
		return x, nil
	}
	return nil, store.ErrNotFound
}
func (f fakeLookup) ObfuscationMapForUpload(_ context.Context, id int64) (*model.SymbolFile, error) {
	if x, ok := f.maps[id]; ok {
		return x, nil
	}
	return nil, store.ErrNotFound
}
func (f fakeLookup) R8MappingForVersion(_ context.Context, _ int64, ver string) (*model.SymbolFile, error) {
	if x, ok := f.r8[ver]; ok {
		return x, nil
	}
	return nil, store.ErrNotFound
}
func (f fakeLookup) EngineSymbolByBuildID(_ context.Context, id string) (*model.EngineSymbol, error) {
	if x, ok := f.engine[id]; ok {
		return x, nil
	}
	return nil, store.ErrNotFound
}
func (f fakeLookup) Materialize(_ context.Context, sha string) (string, error) {
	if p, ok := f.files[sha]; ok {
		return p, nil
	}
	return "/dev/null", nil
}
func (f fakeLookup) TextStart(string) (uint64, error) {
	if f.text == 0 {
		return 0x300000, nil
	}
	return f.text, nil
}

type fakeRepo struct {
	last store.CrashResult
	n    int
}

func (r *fakeRepo) Save(res store.CrashResult) {
	r.n++
	r.last = res
}

func TestProcessPerKind(t *testing.T) {
	ctx := context.Background()
	decodedDart := "#0      _About.build.<anonymous closure> (/apps/kungal/lib/features/me/presentation/me_screen.dart:301:15)\n"
	undecoded := "*** *** *** *** *** *** *** *** *** *** *** *** *** *** *** ***\nbuild_id: '00c92cc1f2db62bbc0baf45738c85488'\n    #00 abs 00000000007ea257 virt 00000000007ea257 _kDartSnapshotText+0x4ea257\n"
	javaRaw := "java.lang.IllegalStateException: boom\n\tat d70.f(SourceFile:5)\n"
	javaOut := "java.lang.IllegalStateException: boom\n\tat dev.nextmoe.Foo.bar(Foo.kt:1)\n"
	anr := "\"main\" prio=5 tid=1 Native\n  at d70.f(SourceFile:5)\n"
	nativeApp := `#00 pc 00000000007ea257  /data/app/x/lib/arm64/libapp.so (BuildId: 00c92cc1f2db62bbc0baf45738c85488)`
	nativeEng := `#00 pc 0000000000001234  /data/app/x/lib/arm64/libflutter.so (BuildId: abcdefabcdefabcdefabcdefabcdefab)`

	job := func(kind string, attrs map[string]any) *store.ClaimedCrash {
		return &store.ClaimedCrash{
			Crash:      model.Crash{AppID: 1, Kind: kind, ServiceVersion: "1.0.0"},
			Attributes: attrs,
			Prefixes:   []string{"package:kungal/", "dev.nextmoe."},
		}
	}

	t.Run("exception waiting dart", func(t *testing.T) {
		repo := &fakeRepo{}
		res := Process(ctx, job(model.KindException, map[string]any{
			"exception.type": "StateError", "exception.stacktrace": undecoded,
		}), fakeTools{}, fakeLookup{})
		repo.Save(res)
		if res.Status != model.CrashWaitingSymbols || res.Needs != "dart:00c92cc1f2db62bbc0baf45738c85488" {
			t.Fatalf("%s %s", res.Status, res.Needs)
		}
	})
	t.Run("exception done", func(t *testing.T) {
		look := fakeLookup{dart: map[string]*model.SymbolFile{
			"00c92cc1f2db62bbc0baf45738c85488": {SHA256: "s", UploadID: 1},
		}}
		res := Process(ctx, job(model.KindException, map[string]any{
			"exception.type": "StateError", "exception.stacktrace": undecoded,
		}), fakeTools{decodeOut: decodedDart}, look)
		if res.Status != model.CrashDone || res.Needs != "" {
			t.Fatalf("%s %s", res.Status, res.Needs)
		}
		if len(res.Frames) == 0 || res.Frames[0].Function != "_About.build.<anonymous closure>" {
			t.Fatalf("%+v", res.Frames)
		}
	})
	t.Run("contract", func(t *testing.T) {
		res := Process(ctx, job(model.KindContract, map[string]any{
			"exception.type": "StateError", "exception.message": "expected String",
			"exception.stacktrace": decodedDart,
		}), fakeTools{}, fakeLookup{})
		if res.Status != model.CrashDone || res.Fingerprint == "" {
			t.Fatalf("%+v", res)
		}
	})
	t.Run("server", func(t *testing.T) {
		res := Process(ctx, job(model.KindServer, map[string]any{
			"http.request.method": "GET", "url.path": "/v2/x/:id", "http.response.status_code": float64(500),
		}), fakeTools{}, fakeLookup{})
		if res.Status != model.CrashDone || res.Title != "GET /v2/x/:id → 500" {
			t.Fatalf("%+v", res)
		}
	})
	t.Run("java waiting r8", func(t *testing.T) {
		res := Process(ctx, job(model.KindJava, map[string]any{
			"exception.type": "java.lang.IllegalStateException", "exception.stacktrace": javaRaw,
		}), fakeTools{}, fakeLookup{})
		if res.Status != model.CrashWaitingSymbols || res.Needs != "r8:1.0.0" {
			t.Fatalf("%s %s", res.Status, res.Needs)
		}
	})
	t.Run("java done", func(t *testing.T) {
		look := fakeLookup{r8: map[string]*model.SymbolFile{"1.0.0": {SHA256: "m"}}}
		res := Process(ctx, job(model.KindJava, map[string]any{
			"exception.stacktrace": javaRaw,
		}), fakeTools{retraceOut: javaOut}, look)
		if res.Status != model.CrashDone || res.ExceptionType != "java.lang.IllegalStateException" {
			t.Fatalf("%+v", res)
		}
	})
	t.Run("anr waiting r8", func(t *testing.T) {
		res := Process(ctx, job(model.KindANR, map[string]any{"exception.stacktrace": anr}), fakeTools{}, fakeLookup{})
		if res.Status != model.CrashWaitingSymbols || res.Needs != "r8:1.0.0" {
			t.Fatalf("%s %s", res.Status, res.Needs)
		}
	})
	t.Run("native waiting dart", func(t *testing.T) {
		res := Process(ctx, job(model.KindNative, map[string]any{
			"exception.type": "SIGSEGV (SI_USER)", "exception.stacktrace": nativeApp,
		}), fakeTools{}, fakeLookup{})
		if res.Status != model.CrashWaitingSymbols || res.Needs != "dart:00c92cc1f2db62bbc0baf45738c85488" {
			t.Fatalf("%s %s", res.Status, res.Needs)
		}
	})
	t.Run("native waiting engine", func(t *testing.T) {
		res := Process(ctx, job(model.KindNative, map[string]any{
			"exception.type": "SIGSEGV", "exception.stacktrace": nativeEng,
		}), fakeTools{}, fakeLookup{})
		if res.Status != model.CrashWaitingSymbols || res.Needs != "engine:abcdefabcdefabcdefabcdefabcdefab" {
			t.Fatalf("%s %s", res.Status, res.Needs)
		}
	})
	t.Run("tool error attempts", func(t *testing.T) {
		look := fakeLookup{dart: map[string]*model.SymbolFile{
			"00c92cc1f2db62bbc0baf45738c85488": {SHA256: "s"},
		}}
		tools := fakeTools{decodeErr: errors.New("boom")}
		j := job(model.KindException, map[string]any{"exception.stacktrace": undecoded})
		res := Process(ctx, j, tools, look)
		if res.Status != model.CrashPending || res.Attempts != 1 {
			t.Fatalf("try1 %+v", res)
		}
		j.Crash.Attempts = 1
		res = Process(ctx, j, tools, look)
		if res.Status != model.CrashPending || res.Attempts != 2 {
			t.Fatalf("try2 %+v", res)
		}
		j.Crash.Attempts = 2
		res = Process(ctx, j, tools, look)
		if res.Status != model.CrashFailed || res.Attempts != 3 || res.Fingerprint == "" {
			t.Fatalf("try3 %+v", res)
		}
	})
}
