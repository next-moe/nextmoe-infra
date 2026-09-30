package symbolicate

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Tools interface {
	DecodeDart(ctx context.Context, debugPath, stack string) (string, error)
	Retrace(ctx context.Context, mappingPath, stack string) (string, error)
	Symbolize(ctx context.Context, objPath string, pcs []uint64) ([]LLVMAddress, error)
}

const ToolTimeout = 30 * time.Second

type ExecTools struct {
	DecodeBin      string
	JavaBin        string
	R8Jar          string
	LLVMSymbolizer string
}

func (e ExecTools) DecodeDart(ctx context.Context, debugPath, stack string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, ToolTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, e.DecodeBin, debugPath)
	cmd.Stdin = strings.NewReader(stack)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("telemetry-decode: %w", err)
	}
	return stdout.String(), nil
}

func (e ExecTools) Retrace(ctx context.Context, mappingPath, stack string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, ToolTimeout)
	defer cancel()
	tmp, err := os.CreateTemp("", "tel-retrace-*.txt")
	if err != nil {
		return "", err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.WriteString(stack); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, e.JavaBin, "-cp", e.R8Jar, "com.android.tools.r8.retrace.Retrace", mappingPath, name)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("r8 retrace: %w", err)
	}
	return stdout.String(), nil
}

func (e ExecTools) Symbolize(ctx context.Context, objPath string, pcs []uint64) ([]LLVMAddress, error) {
	if len(pcs) == 0 {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, ToolTimeout)
	defer cancel()
	args := []string{"--obj=" + objPath, "--output-style=JSON", "--inlining", "--demangle"}
	for _, pc := range pcs {
		args = append(args, fmt.Sprintf("0x%x", pc))
	}
	cmd := exec.CommandContext(ctx, e.LLVMSymbolizer, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("llvm-symbolizer: %w", err)
	}
	return ParseLLVMJSON(stdout.Bytes())
}
