package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"git-vwt/internal/gitx"
	"git-vwt/internal/vwt"
)

const zeroOID = "0000000000000000000000000000000000000000"

type IO struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

func main() {
	ctx := context.Background()
	code := run(ctx, os.Args[1:], IO{In: os.Stdin, Out: os.Stdout, Err: os.Stderr})
	os.Exit(code)
}

func run(ctx context.Context, argv []string, stdio IO) int {
	if len(argv) == 0 {
		usage(stdio.Out)
		return 2
	}

	debug := false
	wsName := strings.TrimSpace(os.Getenv("VWT_WORKSPACE"))
	agentName := strings.TrimSpace(os.Getenv("VWT_AGENT"))

	for len(argv) > 0 {
		switch argv[0] {
		case "--help", "-h", "help":
			usage(stdio.Out)
			return 0
		case "--debug":
			debug = true
			argv = argv[1:]
		case "--ws":
			if len(argv) < 2 {
				fmt.Fprintln(stdio.Err, "missing value for --ws")
				return 2
			}
			wsName = strings.TrimSpace(argv[1])
			argv = argv[2:]
		case "--agent":
			if len(argv) < 2 {
				fmt.Fprintln(stdio.Err, "missing value for --agent")
				return 2
			}
			agentName = strings.TrimSpace(argv[1])
			argv = argv[2:]
		default:
			goto doneGlobals
		}
	}

doneGlobals:
	if len(argv) == 0 {
		usage(stdio.Out)
		return 2
	}
	if wsName == "" {
		wsName = "default"
	}

	gr := gitx.Runner{Dir: ".", Env: os.Environ(), Debug: debug, DebugWriter: stdio.Err}
	cmd := argv[0]
	args := argv[1:]

	switch cmd {
	case "open":
		return cmdOpen(ctx, gr, wsName, agentName, args, stdio)
	case "info":
		return cmdInfo(ctx, gr, wsName, args, stdio)
	case "read":
		return cmdRead(ctx, gr, wsName, agentName, args, stdio)
	case "write":
		return cmdWrite(ctx, gr, wsName, agentName, args, stdio)
	case "rm":
		return cmdRemove(ctx, gr, wsName, agentName, args, stdio)
	case "mv":
		return cmdMove(ctx, gr, wsName, agentName, args, stdio)
	case "ls":
		return cmdListDir(ctx, gr, wsName, agentName, args, stdio)
	case "search":
		return cmdSearch(ctx, gr, wsName, agentName, args, stdio)
	case "patch":
		return cmdPatch(ctx, gr, wsName, agentName, args, stdio)
	case "apply":
		return cmdApply(ctx, gr, wsName, agentName, args, stdio)
	case "close":
		return cmdClose(ctx, gr, wsName, args, stdio)
	default:
		fmt.Fprintf(stdio.Err, "unknown subcommand: %s\n", cmd)
		usage(stdio.Err)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "git vwt - virtual workspace (no hunks, no worktrees)")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Global flags:")
	fmt.Fprintln(w, "  --ws <name>     Workspace name (default: $VWT_WORKSPACE or 'default')")
	fmt.Fprintln(w, "  --agent <name>  Author name for workspace commits (default: $VWT_AGENT)")
	fmt.Fprintln(w, "  --debug         Print git commands to stderr")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  git vwt open [--base <rev>|auto]   Create workspace if missing")
	fmt.Fprintln(w, "  git vwt info                      Print workspace base/head")
	fmt.Fprintln(w, "  git vwt read <path>               Read file from workspace")
	fmt.Fprintln(w, "  git vwt write <path> [<src-file>] Write file to workspace (stdin if no src)")
	fmt.Fprintln(w, "  git vwt rm <path>                 Delete file in workspace")
	fmt.Fprintln(w, "  git vwt mv <from> <to>            Rename file in workspace")
	fmt.Fprintln(w, "  git vwt ls [path]                 List directory in workspace")
	fmt.Fprintln(w, "  git vwt search <pattern> [-- <pathspec>...]  Search workspace")
	fmt.Fprintln(w, "  git vwt patch                     Print unified diff vs workspace base")
	fmt.Fprintln(w, "  git vwt apply                     Apply workspace changes to working dir")
	fmt.Fprintln(w, "  git vwt close                     Delete workspace ref")
}

func ensureRepo(ctx context.Context, gr gitx.Runner) error {
	_, err := gr.RunGit(ctx, nil, "rev-parse", "--git-dir")
	if err != nil {
		return errors.New("not a git repository (or any of the parent directories)")
	}
	return nil
}

func resolveCommit(ctx context.Context, gr gitx.Runner, rev string) (string, error) {
	res, err := gr.RunGit(ctx, nil, "rev-parse", "--verify", rev+"^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}

func resolveTree(ctx context.Context, gr gitx.Runner, rev string) (string, error) {
	res, err := gr.RunGit(ctx, nil, "rev-parse", "--verify", rev+"^{tree}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}

func checkRefFormat(ctx context.Context, gr gitx.Runner, ref string) error {
	_, err := gr.RunGit(ctx, nil, "check-ref-format", ref)
	return err
}

func refExists(ctx context.Context, gr gitx.Runner, ref string) (bool, error) {
	res, err := gr.RunGit(ctx, nil, "show-ref", "--verify", "--quiet", ref)
	if err == nil {
		return true, nil
	}
	if res.ExitCode == 1 {
		return false, nil
	}
	return false, err
}

func commitParent(ctx context.Context, gr gitx.Runner, commit string) (string, error) {
	res, err := gr.RunGit(ctx, nil, "rev-list", "--parents", "-n", "1", commit)
	if err != nil {
		return "", err
	}
	toks := strings.Fields(res.Stdout)
	if len(toks) < 2 {
		return "", fmt.Errorf("commit has no parent (expected workspace base): %s", commit)
	}
	return toks[1], nil
}

func validateTreePath(p string) error {
	p = strings.TrimSpace(p)
	if p == "" {
		return errors.New("empty path")
	}
	if strings.ContainsAny(p, "\x00\r\n") {
		return fmt.Errorf("refusing invalid path characters: %q", p)
	}
	for strings.HasPrefix(p, "./") {
		p = strings.TrimPrefix(p, "./")
	}
	if p == "" {
		return errors.New("empty path")
	}
	if strings.HasPrefix(p, "/") {
		return fmt.Errorf("refusing absolute path: %s", p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return fmt.Errorf("refusing unsafe path: %s", p)
		}
	}
	if p == ".git" || strings.HasPrefix(p, ".git/") {
		return errors.New("refusing path .git/**")
	}
	return nil
}

type workspace struct {
	Name string
	Ref  string
	Head string
	Base string
}

func wsRef(name string) string {
	return vwt.WorkspaceRef(name)
}

func isDirty(ctx context.Context, gr gitx.Runner) (bool, error) {
	res, err := gr.RunGit(ctx, nil, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(res.Stdout) != "", nil
}

func ensureWorkspace(ctx context.Context, gr gitx.Runner, name, agent, baseArg string) (workspace, error) {
	ref := wsRef(name)
	if err := checkRefFormat(ctx, gr, ref); err != nil {
		return workspace{}, fmt.Errorf("invalid workspace name %q: %w", name, err)
	}

	exists, err := refExists(ctx, gr, ref)
	if err != nil {
		return workspace{}, err
	}
	if exists {
		head, err := resolveCommit(ctx, gr, ref)
		if err != nil {
			return workspace{}, err
		}
		base, err := commitParent(ctx, gr, head)
		if err != nil {
			return workspace{}, err
		}
		return workspace{Name: name, Ref: ref, Head: head, Base: base}, nil
	}

	baseCommit, baseTree, err := selectBase(ctx, gr, name, agent, baseArg)
	if err != nil {
		return workspace{}, err
	}

	head, err := commitTree(ctx, gr, agent, baseTree, []string{baseCommit}, "vwt: ws "+name, "")
	if err != nil {
		return workspace{}, err
	}

	if _, err := gr.RunGit(ctx, nil, "update-ref", "-m", "vwt open "+name, ref, head, zeroOID); err != nil {
		return workspace{}, err
	}
	return workspace{Name: name, Ref: ref, Head: head, Base: baseCommit}, nil
}

func selectBase(ctx context.Context, gr gitx.Runner, wsName, agent, baseArg string) (baseCommit string, baseTree string, err error) {
	baseArg = strings.TrimSpace(baseArg)
	if baseArg != "" && baseArg != "auto" {
		c, err := resolveCommit(ctx, gr, baseArg)
		if err != nil {
			return "", "", fmt.Errorf("resolve base commit: %w", err)
		}
		t, err := resolveTree(ctx, gr, c)
		if err != nil {
			return "", "", fmt.Errorf("resolve base tree: %w", err)
		}
		return c, t, nil
	}

	headCommit, headErr := resolveCommit(ctx, gr, "HEAD")
	if headErr == nil {
		dirty, err := isDirty(ctx, gr)
		if err != nil {
			return "", "", err
		}
		if !dirty {
			t, err := resolveTree(ctx, gr, headCommit)
			if err != nil {
				return "", "", err
			}
			return headCommit, t, nil
		}
	}

	// Dirty repo (or no HEAD): snapshot the working directory.
	parent := ""
	if headErr == nil {
		parent = headCommit
	}
	snapCommit, snapTree, err := snapshotWorkdir(ctx, gr, agent, wsName, parent)
	if err != nil {
		return "", "", err
	}
	return snapCommit, snapTree, nil
}

func snapshotWorkdir(ctx context.Context, gr gitx.Runner, agent, wsName, parentCommit string) (commit string, tree string, err error) {
	tmpDir, err := os.MkdirTemp("", "vwt-snapshot-")
	if err != nil {
		return "", "", err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	idxPath := filepath.Join(tmpDir, "index")
	if err := os.WriteFile(idxPath, nil, 0o600); err != nil {
		return "", "", err
	}

	tmpGit := gr.WithEnv(map[string]string{"GIT_INDEX_FILE": idxPath})
	if parentCommit != "" {
		if _, err := tmpGit.RunGit(ctx, nil, "read-tree", parentCommit); err != nil {
			return "", "", err
		}
	} else {
		if _, err := tmpGit.RunGit(ctx, nil, "read-tree", "--empty"); err != nil {
			return "", "", err
		}
	}

	// Include untracked by default, exclude ignored by default.
	if _, err := tmpGit.RunGit(ctx, nil, "add", "-A"); err != nil {
		return "", "", err
	}
	wr, err := tmpGit.RunGit(ctx, nil, "write-tree")
	if err != nil {
		return "", "", err
	}
	tree = strings.TrimSpace(wr.Stdout)

	parents := []string{}
	if parentCommit != "" {
		parents = []string{parentCommit}
	}
	subject := "vwt: snapshot for ws " + wsName
	body := ""
	commit, err = commitTree(ctx, gr, agent, tree, parents, subject, body)
	if err != nil {
		return "", "", err
	}
	return commit, tree, nil
}

func commitTree(ctx context.Context, gr gitx.Runner, agent, tree string, parents []string, subject, body string) (string, error) {
	tree = strings.TrimSpace(tree)
	if tree == "" {
		return "", errors.New("empty tree")
	}

	now := time.Now().UTC()
	authorName := "vwt"
	if strings.TrimSpace(agent) != "" {
		authorName = strings.TrimSpace(agent)
	}

	commitGit := gr.WithEnv(map[string]string{
		"GIT_AUTHOR_NAME":     authorName,
		"GIT_AUTHOR_EMAIL":    "vwt@local",
		"GIT_COMMITTER_NAME":  "vwt",
		"GIT_COMMITTER_EMAIL": "vwt@local",
		"GIT_AUTHOR_DATE":     now.Format(time.RFC3339),
		"GIT_COMMITTER_DATE":  now.Format(time.RFC3339),
	})

	args := []string{"commit-tree", tree}
	for _, p := range parents {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		args = append(args, "-p", p)
	}
	if strings.TrimSpace(subject) != "" {
		args = append(args, "-m", subject)
	}
	if strings.TrimSpace(body) != "" {
		args = append(args, "-m", body)
	}

	res, err := commitGit.RunGit(ctx, nil, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}

func cmdOpen(ctx context.Context, gr gitx.Runner, wsName, agent string, argv []string, stdio IO) int {
	fs := flag.NewFlagSet("open", flag.ContinueOnError)
	fs.SetOutput(stdio.Err)
	baseArg := fs.String("base", "auto", "Base commit to view (auto = snapshot if dirty, else HEAD)")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stdio.Err, "open: no arguments")
		return 2
	}
	if err := ensureRepo(ctx, gr); err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 2
	}

	ws, err := ensureWorkspace(ctx, gr, wsName, agent, *baseArg)
	if err != nil {
		fmt.Fprintf(stdio.Err, "open: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdio.Out, "%s\t%s\t%s\n", ws.Name, ws.Head, ws.Base)
	return 0
}

func cmdInfo(ctx context.Context, gr gitx.Runner, wsName string, argv []string, stdio IO) int {
	fs := flag.NewFlagSet("info", flag.ContinueOnError)
	fs.SetOutput(stdio.Err)
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stdio.Err, "info: no arguments")
		return 2
	}
	if err := ensureRepo(ctx, gr); err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 2
	}

	ref := wsRef(wsName)
	exists, err := refExists(ctx, gr, ref)
	if err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 1
	}
	if !exists {
		fmt.Fprintf(stdio.Err, "workspace not found: %s\n", wsName)
		return 1
	}
	head, err := resolveCommit(ctx, gr, ref)
	if err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 1
	}
	base, err := commitParent(ctx, gr, head)
	if err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 1
	}
	fmt.Fprintf(stdio.Out, "%s\t%s\t%s\n", wsName, head, base)
	return 0
}

func cmdRead(ctx context.Context, gr gitx.Runner, wsName, agent string, argv []string, stdio IO) int {
	fs := flag.NewFlagSet("read", flag.ContinueOnError)
	fs.SetOutput(stdio.Err)
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stdio.Err, "read: expected <path>")
		return 2
	}
	if err := ensureRepo(ctx, gr); err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 2
	}

	path := strings.TrimSpace(fs.Arg(0))
	if err := validateTreePath(path); err != nil {
		fmt.Fprintf(stdio.Err, "read: %v\n", err)
		return 2
	}

	ws, err := ensureWorkspace(ctx, gr, wsName, agent, "auto")
	if err != nil {
		fmt.Fprintf(stdio.Err, "read: %v\n", err)
		return 1
	}
	res, err := gr.RunGit(ctx, nil, "show", ws.Head+":"+path)
	if err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 1
	}
	fmt.Fprint(stdio.Out, res.Stdout)
	return 0
}

func cmdWrite(ctx context.Context, gr gitx.Runner, wsName, agent string, argv []string, stdio IO) int {
	fs := flag.NewFlagSet("write", flag.ContinueOnError)
	fs.SetOutput(stdio.Err)
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if fs.NArg() != 1 && fs.NArg() != 2 {
		fmt.Fprintln(stdio.Err, "write: expected <path> [<src-file>]")
		return 2
	}
	if err := ensureRepo(ctx, gr); err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 2
	}

	path := strings.TrimSpace(fs.Arg(0))
	if err := validateTreePath(path); err != nil {
		fmt.Fprintf(stdio.Err, "write: %v\n", err)
		return 2
	}

	var r io.Reader = stdio.In
	if fs.NArg() == 2 {
		p := fs.Arg(1)
		f, err := os.Open(p)
		if err != nil {
			fmt.Fprintf(stdio.Err, "write: open %s: %v\n", p, err)
			return 1
		}
		defer f.Close()
		r = f
	}

	ws, err := ensureWorkspace(ctx, gr, wsName, agent, "auto")
	if err != nil {
		fmt.Fprintf(stdio.Err, "write: %v\n", err)
		return 1
	}

	newHead, err := wsWriteBlob(ctx, gr, ws, agent, path, r)
	if err != nil {
		fmt.Fprintf(stdio.Err, "write: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdio.Out, "%s\t%s\t%s\n", ws.Name, newHead, ws.Base)
	return 0
}

func cmdRemove(ctx context.Context, gr gitx.Runner, wsName, agent string, argv []string, stdio IO) int {
	fs := flag.NewFlagSet("rm", flag.ContinueOnError)
	fs.SetOutput(stdio.Err)
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stdio.Err, "rm: expected <path>")
		return 2
	}
	if err := ensureRepo(ctx, gr); err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 2
	}

	path := strings.TrimSpace(fs.Arg(0))
	if err := validateTreePath(path); err != nil {
		fmt.Fprintf(stdio.Err, "rm: %v\n", err)
		return 2
	}

	ws, err := ensureWorkspace(ctx, gr, wsName, agent, "auto")
	if err != nil {
		fmt.Fprintf(stdio.Err, "rm: %v\n", err)
		return 1
	}

	newHead, err := wsRemovePath(ctx, gr, ws, agent, path)
	if err != nil {
		fmt.Fprintf(stdio.Err, "rm: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdio.Out, "%s\t%s\t%s\n", ws.Name, newHead, ws.Base)
	return 0
}

func cmdMove(ctx context.Context, gr gitx.Runner, wsName, agent string, argv []string, stdio IO) int {
	fs := flag.NewFlagSet("mv", flag.ContinueOnError)
	fs.SetOutput(stdio.Err)
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stdio.Err, "mv: expected <from> <to>")
		return 2
	}
	if err := ensureRepo(ctx, gr); err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 2
	}

	from := strings.TrimSpace(fs.Arg(0))
	to := strings.TrimSpace(fs.Arg(1))
	if err := validateTreePath(from); err != nil {
		fmt.Fprintf(stdio.Err, "mv: %v\n", err)
		return 2
	}
	if err := validateTreePath(to); err != nil {
		fmt.Fprintf(stdio.Err, "mv: %v\n", err)
		return 2
	}

	ws, err := ensureWorkspace(ctx, gr, wsName, agent, "auto")
	if err != nil {
		fmt.Fprintf(stdio.Err, "mv: %v\n", err)
		return 1
	}

	newHead, err := wsMovePath(ctx, gr, ws, agent, from, to)
	if err != nil {
		fmt.Fprintf(stdio.Err, "mv: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdio.Out, "%s\t%s\t%s\n", ws.Name, newHead, ws.Base)
	return 0
}

func cmdListDir(ctx context.Context, gr gitx.Runner, wsName, agent string, argv []string, stdio IO) int {
	fs := flag.NewFlagSet("ls", flag.ContinueOnError)
	fs.SetOutput(stdio.Err)
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if fs.NArg() > 1 {
		fmt.Fprintln(stdio.Err, "ls: expected [path]")
		return 2
	}
	if err := ensureRepo(ctx, gr); err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 2
	}

	path := ""
	if fs.NArg() == 1 {
		path = strings.TrimSpace(fs.Arg(0))
		if path != "" && path != "." {
			if err := validateTreePath(path); err != nil {
				fmt.Fprintf(stdio.Err, "ls: %v\n", err)
				return 2
			}
		}
	}

	ws, err := ensureWorkspace(ctx, gr, wsName, agent, "auto")
	if err != nil {
		fmt.Fprintf(stdio.Err, "ls: %v\n", err)
		return 1
	}

	if path == "" || path == "." {
		res, err := gr.RunGit(ctx, nil, "ls-tree", "--name-only", ws.Head)
		if err != nil {
			fmt.Fprintln(stdio.Err, err)
			return 1
		}
		fmt.Fprint(stdio.Out, res.Stdout)
		return 0
	}

	objType, err := catFileType(ctx, gr, ws.Head+":"+path)
	if err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 1
	}
	if objType != "tree" {
		fmt.Fprintln(stdio.Out, path)
		return 0
	}

	res, err := gr.RunGit(ctx, nil, "ls-tree", "--name-only", ws.Head+":"+path)
	if err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 1
	}
	fmt.Fprint(stdio.Out, res.Stdout)
	return 0
}

func cmdSearch(ctx context.Context, gr gitx.Runner, wsName, agent string, argv []string, stdio IO) int {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.SetOutput(stdio.Err)
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(stdio.Err, "search: expected <pattern> [-- <pathspec>...]")
		return 2
	}
	if err := ensureRepo(ctx, gr); err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 2
	}

	pattern := fs.Arg(0)
	pathspec := fs.Args()[1:]

	ws, err := ensureWorkspace(ctx, gr, wsName, agent, "auto")
	if err != nil {
		fmt.Fprintf(stdio.Err, "search: %v\n", err)
		return 1
	}

	args := []string{"grep", "--no-color", "-n", "--full-name", pattern, ws.Head, "--"}
	args = append(args, pathspec...)
	res, err := gr.RunGit(ctx, nil, args...)
	if err != nil {
		// git grep exit 1 means no matches; treat as success.
		if res.ExitCode == 1 {
			return 0
		}
		fmt.Fprintln(stdio.Err, err)
		return 1
	}
	fmt.Fprint(stdio.Out, res.Stdout)
	return 0
}

func cmdPatch(ctx context.Context, gr gitx.Runner, wsName, agent string, argv []string, stdio IO) int {
	fs := flag.NewFlagSet("patch", flag.ContinueOnError)
	fs.SetOutput(stdio.Err)
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stdio.Err, "patch: no arguments")
		return 2
	}
	if err := ensureRepo(ctx, gr); err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 2
	}

	ws, err := ensureWorkspace(ctx, gr, wsName, agent, "auto")
	if err != nil {
		fmt.Fprintf(stdio.Err, "patch: %v\n", err)
		return 1
	}
	res, err := gitDiff(ctx, gr, ws.Base, ws.Head)
	if err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 1
	}
	fmt.Fprint(stdio.Out, res)
	return 0
}

func cmdApply(ctx context.Context, gr gitx.Runner, wsName, agent string, argv []string, stdio IO) int {
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	fs.SetOutput(stdio.Err)
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stdio.Err, "apply: no arguments")
		return 2
	}
	if err := ensureRepo(ctx, gr); err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 2
	}

	ws, err := ensureWorkspace(ctx, gr, wsName, agent, "auto")
	if err != nil {
		fmt.Fprintf(stdio.Err, "apply: %v\n", err)
		return 1
	}
	diff, err := gitDiff(ctx, gr, ws.Base, ws.Head)
	if err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 1
	}
	if strings.TrimSpace(diff) == "" {
		return 0
	}
	// First, try a strict apply.
	if _, err := gr.RunGit(ctx, strings.NewReader(diff), "apply", "--whitespace=nowarn", "--recount"); err == nil {
		return 0
	}

	// If strict apply fails, fall back to a three-way apply that can write conflict markers.
	//
	// Note: `git apply --3way` requires affected files to exist in the index. To avoid touching the
	// user's index, we use a temporary index file and stage the current working tree versions of
	// changed paths into it.
	paths, err := gitDiffNameOnly(ctx, gr, ws.Base, ws.Head)
	if err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 1
	}

	tmp, err := os.CreateTemp("", "git-vwt-index-")
	if err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 1
	}
	idxPath := tmp.Name()
	_ = tmp.Close()
	// git commands expect a valid index file. We remove the pre-created file and let git create it.
	_ = os.Remove(idxPath)
	defer func() { _ = os.Remove(idxPath) }()

	idxRunner := gr.WithEnv(map[string]string{"GIT_INDEX_FILE": idxPath})
	if _, err := idxRunner.RunGit(ctx, nil, "read-tree", "--empty"); err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 1
	}

	for _, p := range paths {
		if err := validateTreePath(p); err != nil {
			fmt.Fprintln(stdio.Err, err)
			return 1
		}
		if _, statErr := os.Stat(p); statErr != nil {
			// Deleted paths won't exist in the working tree. Skip staging.
			continue
		}
		if _, err := idxRunner.RunGit(ctx, nil, "add", "--", p); err != nil {
			fmt.Fprintln(stdio.Err, err)
			return 1
		}
	}

	res3, err := idxRunner.RunGit(ctx, strings.NewReader(diff), "apply", "--3way", "--whitespace=nowarn", "--recount")
	combined := res3.Stdout + res3.Stderr
	// Exit code 1 with a conflict summary means the patch was applied with conflict markers.
	if res3.ExitCode == 1 && strings.Contains(combined, "with conflicts") {
		if strings.TrimSpace(res3.Stdout) != "" {
			fmt.Fprint(stdio.Out, res3.Stdout)
		}
		if strings.TrimSpace(res3.Stderr) != "" {
			fmt.Fprint(stdio.Err, res3.Stderr)
		}
		return 1
	}
	if err == nil {
		return 0
	}
	fmt.Fprintln(stdio.Err, err)
	return 1
}

func gitDiffNameOnly(ctx context.Context, gr gitx.Runner, base, head string) ([]string, error) {
	res, err := gr.RunGit(ctx, nil, "diff", "--name-only", "--no-renames", "--no-ext-diff", "--no-textconv", base, head)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0)
	for _, line := range strings.Split(res.Stdout, "\n") {
		p := strings.TrimSpace(line)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

func cmdClose(ctx context.Context, gr gitx.Runner, wsName string, argv []string, stdio IO) int {
	fs := flag.NewFlagSet("close", flag.ContinueOnError)
	fs.SetOutput(stdio.Err)
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stdio.Err, "close: no arguments")
		return 2
	}
	if err := ensureRepo(ctx, gr); err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 2
	}

	ref := wsRef(wsName)
	exists, err := refExists(ctx, gr, ref)
	if err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 1
	}
	if !exists {
		return 0
	}
	head, err := resolveCommit(ctx, gr, ref)
	if err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 1
	}
	if _, err := gr.RunGit(ctx, nil, "update-ref", "-d", "-m", "vwt close "+wsName, ref, head); err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 1
	}
	return 0
}

func gitDiff(ctx context.Context, gr gitx.Runner, base, head string) (string, error) {
	res, err := gr.RunGit(ctx, nil, "diff",
		"--no-color",
		"--binary",
		"--full-index",
		"--no-renames",
		"--no-ext-diff",
		"--no-textconv",
		base, head,
	)
	if err != nil {
		return "", err
	}
	return res.Stdout, nil
}

func catFileType(ctx context.Context, gr gitx.Runner, revObj string) (string, error) {
	res, err := gr.RunGit(ctx, nil, "cat-file", "-t", revObj)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}

func lsTreeEntry(ctx context.Context, gr gitx.Runner, commit, path string) (mode, typ, oid string, ok bool, err error) {
	res, err := gr.RunGit(ctx, nil, "ls-tree", commit, "--", path)
	if err != nil {
		return "", "", "", false, err
	}
	line := strings.TrimSpace(res.Stdout)
	if line == "" {
		return "", "", "", false, nil
	}
	// Format: <mode> <type> <oid>\t<path>
	parts := strings.SplitN(line, "\t", 2)
	if len(parts) != 2 {
		return "", "", "", false, fmt.Errorf("unexpected ls-tree output: %q", line)
	}
	meta := strings.Fields(parts[0])
	if len(meta) < 3 {
		return "", "", "", false, fmt.Errorf("unexpected ls-tree meta: %q", parts[0])
	}
	return meta[0], meta[1], meta[2], true, nil
}

func wsWriteBlob(ctx context.Context, gr gitx.Runner, ws workspace, agent, path string, r io.Reader) (string, error) {
	mode, _, _, exists, err := lsTreeEntry(ctx, gr, ws.Head, path)
	if err != nil {
		return "", err
	}
	if !exists {
		mode = "100644"
	}

	// Write blob.
	hashRes, err := gr.RunGit(ctx, r, "hash-object", "-w", "--stdin")
	if err != nil {
		return "", err
	}
	blobOID := strings.TrimSpace(hashRes.Stdout)

	oldTree, err := resolveTree(ctx, gr, ws.Head)
	if err != nil {
		return "", err
	}

	newTree, err := rewriteTree(ctx, gr, ws.Head, func(tmpGit gitx.Runner) error {
		_, err := tmpGit.RunGit(ctx, nil, "update-index", "--add", "--cacheinfo", mode, blobOID, path)
		return err
	})
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(newTree) == strings.TrimSpace(oldTree) {
		return ws.Head, nil
	}

	newHead, err := commitTree(ctx, gr, agent, newTree, []string{ws.Base}, "vwt: write "+path, "")
	if err != nil {
		return "", err
	}
	if _, err := gr.RunGit(ctx, nil, "update-ref", "-m", "vwt write "+path, ws.Ref, newHead, ws.Head); err != nil {
		return "", err
	}
	return newHead, nil
}

func wsRemovePath(ctx context.Context, gr gitx.Runner, ws workspace, agent, path string) (string, error) {
	oldTree, err := resolveTree(ctx, gr, ws.Head)
	if err != nil {
		return "", err
	}

	newTree, err := rewriteTree(ctx, gr, ws.Head, func(tmpGit gitx.Runner) error {
		_, err := tmpGit.RunGit(ctx, nil, "update-index", "--remove", "--", path)
		return err
	})
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(newTree) == strings.TrimSpace(oldTree) {
		return ws.Head, nil
	}

	newHead, err := commitTree(ctx, gr, agent, newTree, []string{ws.Base}, "vwt: rm "+path, "")
	if err != nil {
		return "", err
	}
	if _, err := gr.RunGit(ctx, nil, "update-ref", "-m", "vwt rm "+path, ws.Ref, newHead, ws.Head); err != nil {
		return "", err
	}
	return newHead, nil
}

func wsMovePath(ctx context.Context, gr gitx.Runner, ws workspace, agent, from, to string) (string, error) {
	mode, typ, oid, ok, err := lsTreeEntry(ctx, gr, ws.Head, from)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("source path not found: %s", from)
	}
	if typ == "tree" {
		return "", fmt.Errorf("mv of directories is not supported: %s", from)
	}
	if _, _, _, ok, err := lsTreeEntry(ctx, gr, ws.Head, to); err != nil {
		return "", err
	} else if ok {
		return "", fmt.Errorf("destination already exists: %s", to)
	}

	oldTree, err := resolveTree(ctx, gr, ws.Head)
	if err != nil {
		return "", err
	}

	newTree, err := rewriteTree(ctx, gr, ws.Head, func(tmpGit gitx.Runner) error {
		if _, err := tmpGit.RunGit(ctx, nil, "update-index", "--remove", "--", from); err != nil {
			return err
		}
		_, err := tmpGit.RunGit(ctx, nil, "update-index", "--add", "--cacheinfo", mode, oid, to)
		return err
	})
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(newTree) == strings.TrimSpace(oldTree) {
		return ws.Head, nil
	}

	newHead, err := commitTree(ctx, gr, agent, newTree, []string{ws.Base}, "vwt: mv "+from+" -> "+to, "")
	if err != nil {
		return "", err
	}
	if _, err := gr.RunGit(ctx, nil, "update-ref", "-m", "vwt mv "+from, ws.Ref, newHead, ws.Head); err != nil {
		return "", err
	}
	return newHead, nil
}

func rewriteTree(ctx context.Context, gr gitx.Runner, fromCommit string, mutate func(tmpGit gitx.Runner) error) (string, error) {
	tmpDir, err := os.MkdirTemp("", "vwt-index-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	idxPath := filepath.Join(tmpDir, "index")
	if err := os.WriteFile(idxPath, nil, 0o600); err != nil {
		return "", err
	}

	tmpGit := gr.WithEnv(map[string]string{"GIT_INDEX_FILE": idxPath})
	if _, err := tmpGit.RunGit(ctx, nil, "read-tree", fromCommit); err != nil {
		return "", err
	}
	if err := mutate(tmpGit); err != nil {
		return "", err
	}
	wr, err := tmpGit.RunGit(ctx, nil, "write-tree")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(wr.Stdout), nil
}
