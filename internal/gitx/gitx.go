package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"
)

type Runner struct {
	Dir         string
	Env         []string
	Debug       bool
	DebugWriter io.Writer
}

type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

func (r Runner) WithEnv(kv map[string]string) Runner {
	out := r
	out.Env = mergeEnv(out.Env, kv)
	return out
}

func (r Runner) RunGit(ctx context.Context, stdin io.Reader, args ...string) (Result, error) {
	var outBuf, errBuf bytes.Buffer

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = r.Dir
	cmd.Env = r.Env
	cmd.Stdin = stdin
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if r.Debug && r.DebugWriter != nil {
		fmt.Fprintf(r.DebugWriter, "+ git %s\n", shellQuote(args))
	}

	err := cmd.Run()
	res := Result{Stdout: outBuf.String(), Stderr: errBuf.String(), ExitCode: 0}
	if err == nil {
		return res, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		res.ExitCode = exitErr.ExitCode()
	} else {
		res.ExitCode = 1
	}
	return res, fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(res.Stderr))
}

func mergeEnv(base []string, kv map[string]string) []string {
	if len(kv) == 0 {
		return base
	}

	m := make(map[string]string, len(base)+len(kv))
	order := make([]string, 0, len(base)+len(kv))

	for _, e := range base {
		k, v, ok := strings.Cut(e, "=")
		if !ok {
			continue
		}
		if _, seen := m[k]; !seen {
			order = append(order, k)
		}
		m[k] = v
	}
	for k := range kv {
		if _, seen := m[k]; !seen {
			order = append(order, k)
		}
	}
	for k, v := range kv {
		m[k] = v
	}

	// Keep output stable for tests and debug output.
	sort.Strings(order)

	out := make([]string, 0, len(order))
	for _, k := range order {
		out = append(out, k+"="+m[k])
	}
	return out
}

func shellQuote(args []string) string {
	// Minimal quoting for debug logs only.
	var b strings.Builder
	for i, a := range args {
		if i > 0 {
			b.WriteByte(' ')
		}
		if a == "" {
			b.WriteString("''")
			continue
		}
		if strings.IndexFunc(a, func(r rune) bool {
			return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\'' || r == '"'
		}) == -1 {
			b.WriteString(a)
			continue
		}
		b.WriteByte('\'')
		b.WriteString(strings.ReplaceAll(a, "'", "'\\''"))
		b.WriteByte('\'')
	}
	return b.String()
}
