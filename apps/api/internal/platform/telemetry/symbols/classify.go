package symbols

import (
	"bufio"
	"debug/elf"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
)

const (
	KindDartSymbols        = "dart_symbols"
	KindDartObfuscationMap = "dart_obfuscation_map"
	KindR8Mapping          = "r8_mapping"

	FileARM64Symbols = "app.android-arm64.symbols"
	FileARMSymbols   = "app.android-arm.symbols"
	FileX64Symbols   = "app.android-x64.symbols"
	FileObfuscation  = "obfuscation.map.json"
	FileR8Mapping    = "mapping.txt"

	ArchARM64 = "arm64"
	ArchARM   = "arm"
	ArchX64   = "x64"

	VariantARM64Release = "android-arm64-release"
	VariantARMRelease   = "android-arm-release"

	StatusPending = "pending"
	StatusReady   = "ready"
	StatusFailed  = "failed"
)

func ClassifyFileName(name string) (kind, arch string, ok bool) {
	switch name {
	case FileARM64Symbols:
		return KindDartSymbols, ArchARM64, true
	case FileARMSymbols:
		return KindDartSymbols, ArchARM, true
	case FileX64Symbols:
		return KindDartSymbols, ArchX64, true
	case FileObfuscation:
		return KindDartObfuscationMap, "", true
	case FileR8Mapping:
		return KindR8Mapping, "", true
	default:
		return "", "", false
	}
}

func wantMachine(arch string) (elf.Machine, bool) {
	switch arch {
	case ArchARM64:
		return elf.EM_AARCH64, true
	case ArchARM:
		return elf.EM_ARM, true
	case ArchX64:
		return elf.EM_X86_64, true
	default:
		return 0, false
	}
}

func ValidateFile(path, fileName string) (kind, arch, buildID string, err error) {
	kind, arch, ok := ClassifyFileName(fileName)
	if !ok {
		return "", "", "", fmt.Errorf("unknown file name")
	}
	switch kind {
	case KindDartSymbols:
		id, err := validateDartSymbols(path, arch)
		return kind, arch, id, err
	case KindDartObfuscationMap:
		f, err := os.Open(path)
		if err != nil {
			return kind, arch, "", err
		}
		defer f.Close()
		return kind, arch, "", ValidateObfuscationMap(f)
	case KindR8Mapping:
		f, err := os.Open(path)
		if err != nil {
			return kind, arch, "", err
		}
		defer f.Close()
		return kind, arch, "", ValidateR8Mapping(f)
	default:
		return "", "", "", fmt.Errorf("unknown file name")
	}
}

func validateDartSymbols(path, arch string) (string, error) {
	want, ok := wantMachine(arch)
	if !ok {
		return "", fmt.Errorf("unknown arch")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	id, machine, err := BuildIDFromELF(f)
	if err != nil {
		return "", err
	}
	if machine != want {
		return "", fmt.Errorf("elf machine %s, want %s", machine, want)
	}
	return id, nil
}

func ValidateObfuscationMap(r io.Reader) error {
	dec := json.NewDecoder(r)
	var arr []string
	if err := dec.Decode(&arr); err != nil {
		return fmt.Errorf("not a JSON array of strings")
	}
	if len(arr)%2 != 0 {
		return fmt.Errorf("odd-length name map")
	}
	return nil
}

var r8Line = regexp.MustCompile(`^\S.* -> \S.*:$`)

func ValidateR8Mapping(r io.Reader) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for sc.Scan() {
		if r8Line.MatchString(sc.Text()) {
			return nil
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return fmt.Errorf("no R8 mapping line")
}
