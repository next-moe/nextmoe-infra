package worker

import (
	"context"
	"os"

	"api/internal/platform/telemetry/model"
	"api/internal/platform/telemetry/store"
	"api/internal/platform/telemetry/symbolicate"
)

const MaxAttempts = 3

func Process(ctx context.Context, job *store.ClaimedCrash, tools symbolicate.Tools, look Lookup) store.CrashResult {
	attrs := job.Attributes
	typ := symbolicate.AttrString(attrs, "exception.type")
	msg := symbolicate.AttrString(attrs, "exception.message")
	raw := symbolicate.AttrString(attrs, "exception.stacktrace")
	if raw == "" {
		raw = job.Crash.Stack
	}
	if typ == "" {
		typ = job.Crash.ExceptionType
	}
	if msg == "" {
		msg = job.Crash.Message
	}
	decoded, frames, needs, toolErr := decodeKind(ctx, job.Crash.Kind, job.Crash.AppID, job.Crash.ServiceVersion, typ, msg, raw, tools, look)
	if toolErr != nil {
		res := finishRaw(job, typ, msg, raw)
		res.Attempts = job.Crash.Attempts + 1
		if res.Attempts >= MaxAttempts {
			res.Status = model.CrashFailed
		} else {
			res.Status = model.CrashPending
		}
		return res
	}
	typ, msg = decoded.typ, decoded.msg
	stack := decoded.stack
	fpFrames := frames
	mainFn := ""
	if job.Crash.Kind == model.KindANR {
		sec := symbolicate.MainThreadSection(stack)
		mainFrames := symbolicate.ParseJava(sec)
		if len(mainFrames) > 0 {
			mainFn = mainFrames[0].Function
		}
		fpFrames = mainFrames
		if len(frames) == 0 {
			frames = mainFrames
		}
	}
	selected := symbolicate.SelectFrames(job.Crash.Kind, fpFrames, job.Prefixes)
	in := symbolicate.FingerprintInput{
		Kind:          job.Crash.Kind,
		ExceptionType: typ,
		Message:       msg,
		Method:        symbolicate.AttrString(attrs, "http.request.method"),
		Path:          symbolicate.AttrString(attrs, "url.path"),
		Status:        symbolicate.AttrStatus(attrs, "http.response.status_code"),
		Frames:        selected,
	}
	status := model.CrashDone
	needStr := symbolicate.NeedsList(needs...)
	if needStr != "" {
		status = model.CrashWaitingSymbols
	}
	return store.CrashResult{
		Status:        status,
		Needs:         needStr,
		Attempts:      job.Crash.Attempts,
		ExceptionType: typ,
		Message:       msg,
		Stack:         stack,
		Frames:        frames,
		Fingerprint:   symbolicate.Fingerprint(in),
		Title:         symbolicate.Title(in, mainFn),
		Culprit:       symbolicate.Culprit(job.Crash.Kind, selected),
	}
}

type decoded struct {
	typ, msg, stack string
}

func finishRaw(job *store.ClaimedCrash, typ, msg, raw string) store.CrashResult {
	frames := rawFrames(job.Crash.Kind, raw)
	fpFrames := frames
	mainFn := ""
	if job.Crash.Kind == model.KindANR {
		sec := symbolicate.MainThreadSection(raw)
		fpFrames = symbolicate.ParseJava(sec)
		if len(fpFrames) > 0 {
			mainFn = fpFrames[0].Function
		}
	}
	selected := symbolicate.SelectFrames(job.Crash.Kind, fpFrames, job.Prefixes)
	in := symbolicate.FingerprintInput{
		Kind:          job.Crash.Kind,
		ExceptionType: typ,
		Message:       msg,
		Method:        symbolicate.AttrString(job.Attributes, "http.request.method"),
		Path:          symbolicate.AttrString(job.Attributes, "url.path"),
		Status:        symbolicate.AttrStatus(job.Attributes, "http.response.status_code"),
		Frames:        selected,
	}
	return store.CrashResult{
		ExceptionType: typ,
		Message:       msg,
		Stack:         raw,
		Frames:        frames,
		Fingerprint:   symbolicate.Fingerprint(in),
		Title:         symbolicate.Title(in, mainFn),
		Culprit:       symbolicate.Culprit(job.Crash.Kind, selected),
	}
}

func rawFrames(kind, stack string) []symbolicate.Frame {
	switch kind {
	case model.KindJava, model.KindANR:
		return symbolicate.ParseJava(stack)
	case model.KindNative:
		return symbolicate.ParseDebuggerd(stack)
	default:
		if symbolicate.DartBuildID(stack) != "" {
			undec := symbolicate.ParseUndecodedDart(stack)
			if len(undec) > 0 {
				return undec
			}
		}
		return symbolicate.ParseDecodedDart(stack)
	}
}

func decodeKind(ctx context.Context, kind string, appID int64, version, typ, msg, stack string, tools symbolicate.Tools, look Lookup) (decoded, []symbolicate.Frame, []string, error) {
	switch kind {
	case model.KindJava, model.KindANR:
		return decodeJava(ctx, appID, version, typ, msg, stack, tools, look)
	case model.KindNative:
		return decodeNative(ctx, appID, typ, msg, stack, tools, look)
	default:
		return decodeDart(ctx, appID, typ, msg, stack, tools, look)
	}
}

func decodeDart(ctx context.Context, appID int64, typ, msg, stack string, tools symbolicate.Tools, look Lookup) (decoded, []symbolicate.Frame, []string, error) {
	var needs []string
	outStack := stack
	var frames []symbolicate.Frame
	if id := symbolicate.DartBuildID(stack); id != "" {
		file, err := look.DartSymbolsByBuildID(ctx, appID, id)
		if missing(err) {
			needs = append(needs, "dart:"+id)
			frames = symbolicate.ParseUndecodedDart(stack)
			return decoded{typ, msg, stack}, frames, needs, nil
		}
		if err != nil {
			return decoded{}, nil, nil, err
		}
		path, err := look.Materialize(ctx, file.SHA256)
		if err != nil {
			return decoded{}, nil, nil, err
		}
		got, err := tools.DecodeDart(ctx, path, stack)
		if err != nil {
			return decoded{}, nil, nil, err
		}
		outStack = got
		frames = symbolicate.ParseDecodedDart(got)
		m, err := loadMap(ctx, look, file.UploadID)
		if err != nil {
			return decoded{}, nil, nil, err
		}
		if m != nil {
			typ = symbolicate.DeobfuscateType(typ, m)
			msg = symbolicate.DeobfuscateMessage(msg, m)
		}
	} else {
		frames = symbolicate.ParseDecodedDart(stack)
	}
	return decoded{typ, msg, outStack}, frames, needs, nil
}

func loadMap(ctx context.Context, look Lookup, uploadID int64) (map[string]string, error) {
	file, err := look.ObfuscationMapForUpload(ctx, uploadID)
	if missing(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	path, err := look.Materialize(ctx, file.SHA256)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return symbolicate.ParseObfuscationMap(f)
}

func decodeJava(ctx context.Context, appID int64, version, typ, msg, stack string, tools symbolicate.Tools, look Lookup) (decoded, []symbolicate.Frame, []string, error) {
	file, err := look.R8MappingForVersion(ctx, appID, version)
	if missing(err) {
		return decoded{typ, msg, stack}, symbolicate.ParseJava(stack), []string{"r8:" + version}, nil
	}
	if err != nil {
		return decoded{}, nil, nil, err
	}
	path, err := look.Materialize(ctx, file.SHA256)
	if err != nil {
		return decoded{}, nil, nil, err
	}
	got, err := tools.Retrace(ctx, path, stack)
	if err != nil {
		return decoded{}, nil, nil, err
	}
	rt, rm := symbolicate.ExceptionTypeFromRetrace(got)
	if rt != "" {
		typ = rt
	}
	if msg == "" {
		msg = rm
	}
	return decoded{typ, msg, got}, symbolicate.ParseJava(got), nil, nil
}

func decodeNative(ctx context.Context, appID int64, typ, msg, stack string, tools symbolicate.Tools, look Lookup) (decoded, []symbolicate.Frame, []string, error) {
	frames := symbolicate.ParseDebuggerd(stack)
	var needs []string
	var next []symbolicate.Frame
	for _, f := range frames {
		if f.Module == "libapp.so" && f.BuildID != "" {
			exp, need, err := decodeLibapp(ctx, appID, f, tools, look)
			if err != nil {
				return decoded{}, nil, nil, err
			}
			if need != "" {
				needs = append(needs, need)
				next = append(next, f)
				continue
			}
			next = append(next, exp...)
			continue
		}
		next = append(next, f)
	}
	next, engNeeds, err := decodeFlutter(ctx, next, tools, look)
	if err != nil {
		return decoded{}, nil, nil, err
	}
	needs = append(needs, engNeeds...)
	return decoded{typ, msg, symbolicate.FormatNativeStack(next)}, next, needs, nil
}

func decodeLibapp(ctx context.Context, appID int64, f symbolicate.Frame, tools symbolicate.Tools, look Lookup) ([]symbolicate.Frame, string, error) {
	file, err := look.DartSymbolsByBuildID(ctx, appID, f.BuildID)
	if missing(err) {
		return nil, "dart:" + f.BuildID, nil
	}
	if err != nil {
		return nil, "", err
	}
	path, err := look.Materialize(ctx, file.SHA256)
	if err != nil {
		return nil, "", err
	}
	textStart, err := look.TextStart(path)
	if err != nil {
		return nil, "", err
	}
	synth := symbolicate.SynthesizeDartStack(f.BuildID, textStart, []uint64{f.PC})
	got, err := tools.DecodeDart(ctx, path, synth)
	if err != nil {
		return nil, "", err
	}
	parsed := symbolicate.ParseDecodedDart(got)
	if len(parsed) == 0 {
		return []symbolicate.Frame{f}, "", nil
	}
	for i := range parsed {
		parsed[i].Module = f.Module
		parsed[i].Path = f.Path
		parsed[i].PC = f.PC
		parsed[i].BuildID = f.BuildID
	}
	return parsed, "", nil
}

func decodeFlutter(ctx context.Context, frames []symbolicate.Frame, tools symbolicate.Tools, look Lookup) ([]symbolicate.Frame, []string, error) {
	type group struct {
		pcs []uint64
		sha string
	}
	by := map[string]*group{}
	var needs []string
	seenNeed := map[string]struct{}{}
	for _, f := range frames {
		if f.Module != "libflutter.so" || f.BuildID == "" {
			continue
		}
		if g, ok := by[f.BuildID]; ok {
			if g.sha != "" {
				g.pcs = append(g.pcs, f.PC)
			}
			continue
		}
		eng, err := look.EngineSymbolByBuildID(ctx, f.BuildID)
		if missing(err) {
			n := "engine:" + f.BuildID
			if _, dup := seenNeed[n]; !dup {
				seenNeed[n] = struct{}{}
				needs = append(needs, n)
			}
			by[f.BuildID] = &group{}
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		by[f.BuildID] = &group{pcs: []uint64{f.PC}, sha: eng.SHA256}
	}
	var addrs []symbolicate.LLVMAddress
	for _, g := range by {
		if g.sha == "" || len(g.pcs) == 0 {
			continue
		}
		path, err := look.Materialize(ctx, g.sha)
		if err != nil {
			return nil, nil, err
		}
		a, err := tools.Symbolize(ctx, path, uniquePCs(g.pcs))
		if err != nil {
			return nil, nil, err
		}
		addrs = append(addrs, a...)
	}
	return symbolicate.ApplyLLVM(frames, addrs), needs, nil
}

func uniquePCs(pcs []uint64) []uint64 {
	seen := map[uint64]struct{}{}
	var out []uint64
	for _, p := range pcs {
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}
