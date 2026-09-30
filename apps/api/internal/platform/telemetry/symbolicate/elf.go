package symbolicate

import (
	"debug/elf"
	"fmt"
	"io"
)

const dartTextSymbol = "_kDartSnapshotText"

func TextStart(r io.ReaderAt) (uint64, error) {
	f, err := elf.NewFile(r)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	if v, ok := symbolValue(f.Symbols); ok {
		return v, nil
	}
	if v, ok := symbolValue(f.DynamicSymbols); ok {
		return v, nil
	}
	return 0, fmt.Errorf("%s not found", dartTextSymbol)
}

func symbolValue(fn func() ([]elf.Symbol, error)) (uint64, bool) {
	syms, err := fn()
	if err != nil {
		return 0, false
	}
	for _, s := range syms {
		if s.Name == dartTextSymbol {
			return s.Value, true
		}
	}
	return 0, false
}
