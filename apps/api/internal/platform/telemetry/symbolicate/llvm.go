package symbolicate

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func ParseLLVMJSON(data []byte) ([]LLVMAddress, error) {
	var raw []struct {
		Address    string `json:"Address"`
		ModuleName string `json:"ModuleName"`
		Symbol     []struct {
			FunctionName string `json:"FunctionName"`
			FileName     string `json:"FileName"`
			Line         int    `json:"Line"`
			Column       int    `json:"Column"`
		} `json:"Symbol"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	out := make([]LLVMAddress, 0, len(raw))
	for _, r := range raw {
		addr, err := parseHexAddr(r.Address)
		if err != nil {
			return nil, err
		}
		syms := make([]LLVMSymbol, 0, len(r.Symbol))
		for _, s := range r.Symbol {
			syms = append(syms, LLVMSymbol{
				FunctionName: s.FunctionName,
				FileName:     s.FileName,
				Line:         s.Line,
				Column:       s.Column,
			})
		}
		out = append(out, LLVMAddress{Address: addr, ModuleName: r.ModuleName, Symbols: syms})
	}
	return out, nil
}

func parseHexAddr(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(strings.ToLower(s), "0x")
	if s == "" {
		return 0, fmt.Errorf("empty address")
	}
	return strconv.ParseUint(s, 16, 64)
}

func LLVMUnknown(syms []LLVMSymbol) bool {
	if len(syms) != 1 {
		return false
	}
	s := syms[0]
	return s.FunctionName == "" && s.FileName == "" && s.Line == 0
}

func ExpandLLVMFrame(orig Frame, addrs []LLVMAddress) []Frame {
	var hit *LLVMAddress
	for i := range addrs {
		if addrs[i].Address == orig.PC {
			hit = &addrs[i]
			break
		}
	}
	if hit == nil || LLVMUnknown(hit.Symbols) {
		return []Frame{orig}
	}
	out := make([]Frame, 0, len(hit.Symbols))
	for _, s := range hit.Symbols {
		out = append(out, Frame{
			Function: s.FunctionName,
			File:     dartFileBase(s.FileName),
			Line:     s.Line,
			Module:   orig.Module,
			Path:     orig.Path,
			Raw:      orig.Raw,
			PC:       orig.PC,
			BuildID:  orig.BuildID,
		})
	}
	return out
}

func ApplyLLVM(frames []Frame, addrs []LLVMAddress) []Frame {
	var out []Frame
	for _, f := range frames {
		out = append(out, ExpandLLVMFrame(f, addrs)...)
	}
	return out
}
