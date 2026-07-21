package process

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"os/exec"
)

type NodeRunner struct {
	Binary           string
	Timeout          time.Duration
	MemoryMB         int
	OutputLimitBytes int
}

const defaultOutputLimitBytes = 4 * 1024 * 1024

func (r *NodeRunner) Run(ctx context.Context, script string, args ...string) (string, string, error) {
	return r.RunInDir(ctx, "", script, args...)
}

func (r *NodeRunner) RunInDir(ctx context.Context, dir string, script string, args ...string) (string, string, error) {
	binaryName := r.Binary
	if binaryName == "" {
		binaryName = "node"
	}
	runCtx := ctx
	cancel := func() {}
	if r.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, r.Timeout)
	}
	defer cancel()

	cmdArgs := make([]string, 0, len(args)+2)
	if r.MemoryMB > 0 {
		cmdArgs = append(cmdArgs, "--max-old-space-size="+strconv.Itoa(r.MemoryMB))
	}
	cmdArgs = append(cmdArgs, script)
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.CommandContext(runCtx, binaryName, cmdArgs...)
	if dir != "" {
		cmd.Dir = dir
	}
	outputLimit := r.OutputLimitBytes
	if outputLimit <= 0 {
		outputLimit = defaultOutputLimitBytes
	}
	stdout := newBoundedTailBuffer(outputLimit)
	stderr := newBoundedTailBuffer(outputLimit)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	stdoutText := stdout.String()
	stderrText := stderr.String()
	if err != nil {
		if stderr.TotalBytes() == 0 && stdout.TotalBytes() > 0 {
			stderrText = stdoutText
		}
		return stdoutText, stderrText, fmt.Errorf("node runner: %w", err)
	}
	return stdoutText, stderrText, nil
}

type boundedTailBuffer struct {
	limit int
	data  []byte
	total int64
}

func newBoundedTailBuffer(limit int) boundedTailBuffer {
	if limit < 1 {
		limit = 1
	}
	initialCapacity := limit
	if initialCapacity > 4*1024 {
		initialCapacity = 4 * 1024
	}
	return boundedTailBuffer{limit: limit, data: make([]byte, 0, initialCapacity)}
}

func (b *boundedTailBuffer) Write(p []byte) (int, error) {
	written := len(p)
	b.total += int64(written)
	if written >= b.limit {
		b.data = append(b.data[:0], p[written-b.limit:]...)
		return written, nil
	}
	if overflow := len(b.data) + written - b.limit; overflow > 0 {
		copy(b.data, b.data[overflow:])
		b.data = b.data[:len(b.data)-overflow]
	}
	b.data = append(b.data, p...)
	return written, nil
}

func (b *boundedTailBuffer) TotalBytes() int64 {
	return b.total
}

func (b *boundedTailBuffer) String() string {
	if b.total <= int64(len(b.data)) {
		return string(b.data)
	}
	return fmt.Sprintf("[output truncated: kept last %d of %d bytes]\n%s", len(b.data), b.total, string(b.data))
}

func ResolveExistingPath(candidates ...string) (string, error) {
	searchRoots := buildSearchRoots()
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if filepath.IsAbs(candidate) {
			if _, err := os.Stat(candidate); err == nil {
				return candidate, nil
			}
			continue
		}
		for _, root := range searchRoots {
			fullPath := filepath.Join(root, candidate)
			if _, err := os.Stat(fullPath); err == nil {
				absPath, absErr := filepath.Abs(fullPath)
				if absErr != nil {
					return fullPath, nil
				}
				return absPath, nil
			}
		}
	}
	return "", fmt.Errorf("resource not found: %v", candidates)
}

func buildSearchRoots() []string {
	var roots []string
	seen := make(map[string]struct{})
	addWithParents := func(path string) {
		if path == "" {
			return
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
			return
		}
		current := absPath
		for {
			if _, ok := seen[current]; !ok {
				roots = append(roots, current)
				seen[current] = struct{}{}
			}
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			current = parent
		}
	}

	if cwd, err := os.Getwd(); err == nil {
		addWithParents(cwd)
	}
	if executablePath, err := os.Executable(); err == nil {
		addWithParents(filepath.Dir(executablePath))
	}

	return roots
}
