package main

import (
	"bytes"
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

var (
	osReadFile  = os.ReadFile
	osWriteFile = os.WriteFile
	osMkdirTemp = os.MkdirTemp
	osRemoveAll = os.RemoveAll

	vwtGenerateID = vwt.GenerateID
)

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
	for len(argv) > 0 {
		switch argv[0] {
		case "--help", "-h", "help":
			usage(stdio.Out)
			return 0
		case "--debug":
			debug = true
			argv = argv[1:]
		default:
			goto doneGlobals
		}
	}
doneGlobals:
	if len(argv) == 0 {
		usage(stdio.Out)
		return 2
	}

	gr := gitx.Runner{Dir: ".", Env: os.Environ(), Debug: debug, DebugWriter: stdio.Err}
	cmd := argv[0]
	args := argv[1:]

	switch cmd {
	case "import":
		return cmdImport(ctx, gr, args, stdio)
	case "compose":
		return cmdCompose(ctx, gr, args, stdio)
	case "list":
		return cmdList(ctx, gr, args, stdio)
	case "show":
		return cmdShow(ctx, gr, args, stdio)
	case "diff":
		return cmdDiff(ctx, gr, args, stdio)
	case "export":
		return cmdExport(ctx, gr, args, stdio)
	case "cat":
		return cmdCat(ctx, gr, args, stdio)
	case "apply":
		return cmdApply(ctx, gr, args, stdio)
	case "drop":
		return cmdDrop(ctx, gr, args, stdio)
	case "snapshot":
		return cmdSnapshot(ctx, gr, args, stdio)
	case "gc":
		return cmdGC(ctx, gr, args, stdio)
	default:
		fmt.Fprintf(stdio.Err, "unknown subcommand: %s\n", cmd)
		usage(stdio.Err)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "git vwt - virtual worktree (diff-only) patch inbox")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  git vwt import --base <rev> [--id <id>] [--agent <name>] [--title <title>] [--stdin|<file>]")
	fmt.Fprintln(w, "  git vwt compose --base <rev> [--id <id>] [--agent <name>] [--title <title>] <patch-id>...")
	fmt.Fprintln(w, "  git vwt list")
	fmt.Fprintln(w, "  git vwt show <id>")
	fmt.Fprintln(w, "  git vwt diff <id>")
	fmt.Fprintln(w, "  git vwt export <id>")
	fmt.Fprintln(w, "  git vwt cat <path>")
	fmt.Fprintln(w, "  git vwt cat <id|rev> <path>")
	fmt.Fprintln(w, "  git vwt apply <id>")
	fmt.Fprintln(w, "  git vwt drop <id>")
	fmt.Fprintln(w, "  git vwt snapshot [-m <msg>]")
	fmt.Fprintln(w, "  git vwt gc [--keep-days <n>] [--dry-run]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Global flags:")
	fmt.Fprintln(w, "  --debug   Print git commands to stderr")
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

func cmdImport(ctx context.Context, gr gitx.Runner, argv []string, stdio IO) int {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	fs.SetOutput(stdio.Err)
	base := fs.String("base", "", "Base commit to apply diff against (required)")
	id := fs.String("id", "", "Patch id (ref-safe). Auto-generated if omitted")
	agent := fs.String("agent", "", "Agent name")
	title := fs.String("title", "", "Title")
	stdin := fs.Bool("stdin", false, "Read diff from stdin")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if err := ensureRepo(ctx, gr); err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 2
	}
	if *base == "" {
		fmt.Fprintln(stdio.Err, "import: missing --base")
		return 2
	}

	var raw []byte
	switch {
	case *stdin:
		if fs.NArg() != 0 {
			fmt.Fprintln(stdio.Err, "import: when --stdin is set, do not pass a file path")
			return 2
		}
		b, err := io.ReadAll(stdio.In)
		if err != nil {
			fmt.Fprintf(stdio.Err, "import: read stdin: %v\n", err)
			return 1
		}
		raw = b
	case fs.NArg() == 1:
		p := fs.Arg(0)
		if p == "-" {
			b, err := io.ReadAll(stdio.In)
			if err != nil {
				fmt.Fprintf(stdio.Err, "import: read stdin: %v\n", err)
				return 1
			}
			raw = b
		} else {
			b, err := osReadFile(p)
			if err != nil {
				fmt.Fprintf(stdio.Err, "import: read file: %v\n", err)
				return 1
			}
			raw = b
		}
	default:
		fmt.Fprintln(stdio.Err, "import: provide --stdin or a patch file path")
		return 2
	}

	diff, err := vwt.ExtractDiff(raw)
	if err != nil {
		fmt.Fprintf(stdio.Err, "import: %v\n", err)
		return 1
	}
	if err := vwt.ValidatePatchPaths(diff); err != nil {
		fmt.Fprintf(stdio.Err, "import: %v\n", err)
		return 1
	}

	baseCommit, err := resolveCommit(ctx, gr, *base)
	if err != nil {
		fmt.Fprintf(stdio.Err, "import: resolve base commit: %v\n", err)
		return 1
	}
	baseTree, err := resolveTree(ctx, gr, baseCommit)
	if err != nil {
		fmt.Fprintf(stdio.Err, "import: resolve base tree: %v\n", err)
		return 1
	}

	patchID := strings.TrimSpace(*id)
	if patchID == "" {
		gen, err := vwtGenerateID(time.Now())
		if err != nil {
			fmt.Fprintf(stdio.Err, "import: generate id: %v\n", err)
			return 1
		}
		patchID = gen
	}

	ref := vwt.PatchRef(patchID)
	if err := checkRefFormat(ctx, gr, ref); err != nil {
		fmt.Fprintf(stdio.Err, "import: invalid id for ref %q: %v\n", ref, err)
		return 2
	}
	exists, err := refExists(ctx, gr, ref)
	if err != nil {
		fmt.Fprintf(stdio.Err, "import: check existing ref: %v\n", err)
		return 1
	}
	if exists {
		fmt.Fprintf(stdio.Err, "import: patch id already exists: %s\n", patchID)
		return 1
	}

	tmpDir, err := osMkdirTemp("", "vwt-import-")
	if err != nil {
		fmt.Fprintf(stdio.Err, "import: temp dir: %v\n", err)
		return 1
	}
	defer osRemoveAll(tmpDir)

	idxPath := filepath.Join(tmpDir, "index")
	msgPath := filepath.Join(tmpDir, "msg")

	// Create empty index file.
	if err := osWriteFile(idxPath, nil, 0o600); err != nil {
		fmt.Fprintf(stdio.Err, "import: temp index: %v\n", err)
		return 1
	}

	// Build patch commit in a temporary index.
	tmpGit := gr.WithEnv(map[string]string{"GIT_INDEX_FILE": idxPath})
	if _, err := tmpGit.RunGit(ctx, nil, "read-tree", baseCommit); err != nil {
		fmt.Fprintf(stdio.Err, "import: git read-tree: %v\n", err)
		return 1
	}
	if _, err := tmpGit.RunGit(ctx, bytes.NewReader(diff), "apply", "--cached", "--whitespace=nowarn", "--recount"); err != nil {
		fmt.Fprintf(stdio.Err, "import: git apply: %v\n", err)
		return 1
	}
	treeRes, err := tmpGit.RunGit(ctx, nil, "write-tree")
	if err != nil {
		fmt.Fprintf(stdio.Err, "import: git write-tree: %v\n", err)
		return 1
	}
	newTree := strings.TrimSpace(treeRes.Stdout)
	if newTree == baseTree {
		fmt.Fprintln(stdio.Err, "import: patch results in no changes")
		return 1
	}

	now := time.Now().UTC()
	subject := "vwt: patch " + patchID
	if strings.TrimSpace(*title) != "" {
		if strings.TrimSpace(*agent) != "" {
			subject = fmt.Sprintf("vwt(%s): %s", strings.TrimSpace(*agent), strings.TrimSpace(*title))
		} else {
			subject = fmt.Sprintf("vwt: %s", strings.TrimSpace(*title))
		}
	}
	body := buildKV(map[string]string{
		"vwt-id":           patchID,
		"vwt-agent":        strings.TrimSpace(*agent),
		"vwt-title":        strings.TrimSpace(*title),
		"vwt-base":         baseCommit,
		"vwt-imported-at":  now.Format(time.RFC3339),
		"vwt-diff-sha256":  vwt.PatchSHA256Hex(diff),
		"vwt-tool":         "git-vwt",
		"vwt-tool-version": "(dev)",
	})
	msg := subject + "\n\n" + body + "\n"
	if err := osWriteFile(msgPath, []byte(msg), 0o600); err != nil {
		fmt.Fprintf(stdio.Err, "import: write message: %v\n", err)
		return 1
	}

	authorName := "vwt"
	if strings.TrimSpace(*agent) != "" {
		authorName = strings.TrimSpace(*agent)
	}

	commitGit := tmpGit.WithEnv(map[string]string{
		"GIT_AUTHOR_NAME":     authorName,
		"GIT_AUTHOR_EMAIL":    "vwt@local",
		"GIT_COMMITTER_NAME":  "vwt",
		"GIT_COMMITTER_EMAIL": "vwt@local",
		"GIT_AUTHOR_DATE":     now.Format(time.RFC3339),
		"GIT_COMMITTER_DATE":  now.Format(time.RFC3339),
	})
	commitRes, err := commitGit.RunGit(ctx, nil, "commit-tree", newTree, "-p", baseCommit, "-F", msgPath)
	if err != nil {
		fmt.Fprintf(stdio.Err, "import: git commit-tree: %v\n", err)
		return 1
	}
	commit := strings.TrimSpace(commitRes.Stdout)

	// Create ref atomically (no clobber) by requiring an all-zero old value.
	if _, err := gr.RunGit(ctx, nil, "update-ref", "-m", "vwt import "+patchID, ref, commit, zeroOID); err != nil {
		if exists, exErr := refExists(ctx, gr, ref); exErr == nil && exists {
			fmt.Fprintf(stdio.Err, "import: patch id already exists: %s\n", patchID)
			return 1
		}
		fmt.Fprintf(stdio.Err, "import: git update-ref: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdio.Out, "%s\t%s\t%s\n", patchID, commit, baseCommit)
	return 0
}

func cmdCompose(ctx context.Context, gr gitx.Runner, argv []string, stdio IO) int {
	fs := flag.NewFlagSet("compose", flag.ContinueOnError)
	fs.SetOutput(stdio.Err)
	base := fs.String("base", "", "Base commit to compose against (required)")
	id := fs.String("id", "", "Compose id (ref-safe). Auto-generated if omitted")
	agent := fs.String("agent", "", "Agent name")
	title := fs.String("title", "", "Title")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if err := ensureRepo(ctx, gr); err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 2
	}
	if *base == "" {
		fmt.Fprintln(stdio.Err, "compose: missing --base")
		return 2
	}
	if fs.NArg() == 0 {
		fmt.Fprintln(stdio.Err, "compose: expected <patch-id>...")
		return 2
	}

	baseCommit, err := resolveCommit(ctx, gr, *base)
	if err != nil {
		fmt.Fprintf(stdio.Err, "compose: resolve base commit: %v\n", err)
		return 1
	}
	baseTree, err := resolveTree(ctx, gr, baseCommit)
	if err != nil {
		fmt.Fprintf(stdio.Err, "compose: resolve base tree: %v\n", err)
		return 1
	}

	composeID := strings.TrimSpace(*id)
	if composeID == "" {
		gen, err := vwtGenerateID(time.Now())
		if err != nil {
			fmt.Fprintf(stdio.Err, "compose: generate id: %v\n", err)
			return 1
		}
		composeID = gen
	}

	ref := vwt.PatchRef(composeID)
	if err := checkRefFormat(ctx, gr, ref); err != nil {
		fmt.Fprintf(stdio.Err, "compose: invalid id for ref %q: %v\n", ref, err)
		return 2
	}
	exists, err := refExists(ctx, gr, ref)
	if err != nil {
		fmt.Fprintf(stdio.Err, "compose: check existing ref: %v\n", err)
		return 1
	}
	if exists {
		fmt.Fprintf(stdio.Err, "compose: id already exists: %s\n", composeID)
		return 1
	}

	tmpDir, err := osMkdirTemp("", "vwt-compose-")
	if err != nil {
		fmt.Fprintf(stdio.Err, "compose: temp dir: %v\n", err)
		return 1
	}
	defer osRemoveAll(tmpDir)

	idxPath := filepath.Join(tmpDir, "index")
	msgPath := filepath.Join(tmpDir, "msg")
	if err := osWriteFile(idxPath, nil, 0o600); err != nil {
		fmt.Fprintf(stdio.Err, "compose: temp index: %v\n", err)
		return 1
	}

	// Compose in a temporary index (does not touch the working tree).
	tmpGit := gr.WithEnv(map[string]string{"GIT_INDEX_FILE": idxPath})
	if _, err := tmpGit.RunGit(ctx, nil, "read-tree", baseCommit); err != nil {
		fmt.Fprintf(stdio.Err, "compose: git read-tree: %v\n", err)
		return 1
	}

	patchIDs := fs.Args()
	for _, pid := range patchIDs {
		pid = strings.TrimSpace(pid)
		if pid == "" {
			fmt.Fprintln(stdio.Err, "compose: empty patch id")
			return 2
		}

		patchCommit, err := resolveCommit(ctx, gr, vwt.PatchRef(pid))
		if err != nil {
			fmt.Fprintf(stdio.Err, "compose: unknown patch id: %s\n", pid)
			return 1
		}
		patchBase, err := commitParent(ctx, gr, patchCommit)
		if err != nil {
			fmt.Fprintf(stdio.Err, "compose: patch %s: %v\n", pid, err)
			return 1
		}

		diffRes, err := gr.RunGit(ctx, nil, "diff",
			"--no-color",
			"--binary",
			"--full-index",
			"--no-renames",
			"--no-ext-diff",
			"--no-textconv",
			patchBase, patchCommit,
		)
		if err != nil {
			fmt.Fprintf(stdio.Err, "compose: patch %s: git diff: %v\n", pid, err)
			return 1
		}
		d := []byte(diffRes.Stdout)
		if err := vwt.ValidatePatchPaths(d); err != nil {
			fmt.Fprintf(stdio.Err, "compose: patch %s: %v\n", pid, err)
			return 1
		}
		if _, err := tmpGit.RunGit(ctx, bytes.NewReader(d), "apply", "--cached", "--whitespace=nowarn", "--recount"); err != nil {
			fmt.Fprintf(stdio.Err, "compose: apply %s: %v\n", pid, err)
			return 1
		}
	}

	treeRes, err := tmpGit.RunGit(ctx, nil, "write-tree")
	if err != nil {
		fmt.Fprintf(stdio.Err, "compose: git write-tree: %v\n", err)
		return 1
	}
	newTree := strings.TrimSpace(treeRes.Stdout)
	if newTree == baseTree {
		fmt.Fprintln(stdio.Err, "compose: results in no changes")
		return 1
	}

	now := time.Now().UTC()
	agentName := strings.TrimSpace(*agent)
	titleText := strings.TrimSpace(*title)
	subject := "vwt: compose " + composeID
	if titleText != "" {
		if agentName != "" {
			subject = fmt.Sprintf("vwt(%s): compose %s", agentName, titleText)
		} else {
			subject = fmt.Sprintf("vwt: compose %s", titleText)
		}
	}
	body := buildKV(map[string]string{
		"vwt-id":            composeID,
		"vwt-kind":          "compose",
		"vwt-agent":         agentName,
		"vwt-title":         titleText,
		"vwt-base":          baseCommit,
		"vwt-compose-count": fmt.Sprintf("%d", len(patchIDs)),
		"vwt-compose-ids":   strings.Join(patchIDs, ","),
		"vwt-created-at":    now.Format(time.RFC3339),
		"vwt-tool":          "git-vwt",
		"vwt-tool-version":  "(dev)",
	})
	msg := subject + "\n\n" + body + "\n"
	if err := osWriteFile(msgPath, []byte(msg), 0o600); err != nil {
		fmt.Fprintf(stdio.Err, "compose: write message: %v\n", err)
		return 1
	}

	authorName := "vwt"
	if agentName != "" {
		authorName = agentName
	}
	commitGit := tmpGit.WithEnv(map[string]string{
		"GIT_AUTHOR_NAME":     authorName,
		"GIT_AUTHOR_EMAIL":    "vwt@local",
		"GIT_COMMITTER_NAME":  "vwt",
		"GIT_COMMITTER_EMAIL": "vwt@local",
		"GIT_AUTHOR_DATE":     now.Format(time.RFC3339),
		"GIT_COMMITTER_DATE":  now.Format(time.RFC3339),
	})
	commitRes, err := commitGit.RunGit(ctx, nil, "commit-tree", newTree, "-p", baseCommit, "-F", msgPath)
	if err != nil {
		fmt.Fprintf(stdio.Err, "compose: git commit-tree: %v\n", err)
		return 1
	}
	commit := strings.TrimSpace(commitRes.Stdout)

	// Create ref atomically (no clobber) by requiring an all-zero old value.
	if _, err := gr.RunGit(ctx, nil, "update-ref", "-m", "vwt compose "+composeID, ref, commit, zeroOID); err != nil {
		if exists, exErr := refExists(ctx, gr, ref); exErr == nil && exists {
			fmt.Fprintf(stdio.Err, "compose: id already exists: %s\n", composeID)
			return 1
		}
		fmt.Fprintf(stdio.Err, "compose: git update-ref: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdio.Out, "%s\t%s\t%s\n", composeID, commit, baseCommit)
	return 0
}

func cmdList(ctx context.Context, gr gitx.Runner, argv []string, stdio IO) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(stdio.Err)
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stdio.Err, "list: no arguments")
		return 2
	}
	if err := ensureRepo(ctx, gr); err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 2
	}

	res, err := gr.RunGit(ctx, nil, "for-each-ref", vwt.PatchRefPrefix, "--sort=-committerdate", "--format=%(refname:strip=3)\t%(objectname)\t%(committerdate:iso8601)\t%(subject)")
	if err != nil {
		// No refs is not an error; for-each-ref still exits 0.
		fmt.Fprintln(stdio.Err, err)
		return 1
	}
	out := strings.TrimSpace(res.Stdout)
	if out == "" {
		return 0
	}

	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) < 4 {
			continue
		}
		id, commit, date, subject := parts[0], parts[1], parts[2], parts[3]
		base := "-"
		parents, _ := gr.RunGit(ctx, nil, "rev-list", "--parents", "-n", "1", commit)
		toks := strings.Fields(parents.Stdout)
		if len(toks) >= 2 {
			base = toks[1]
		}
		fmt.Fprintf(stdio.Out, "%s\t%s\t%s\t%s\n", id, date, shortSHA(base), subject)
	}
	return 0
}

func cmdShow(ctx context.Context, gr gitx.Runner, argv []string, stdio IO) int {
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	fs.SetOutput(stdio.Err)
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stdio.Err, "show: expected <id>")
		return 2
	}
	if err := ensureRepo(ctx, gr); err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 2
	}
	id := fs.Arg(0)
	ref := vwt.PatchRef(id)
	commit, err := resolveCommit(ctx, gr, ref)
	if err != nil {
		fmt.Fprintf(stdio.Err, "show: unknown patch id: %s\n", id)
		return 1
	}
	parents, _ := gr.RunGit(ctx, nil, "rev-list", "--parents", "-n", "1", commit)
	toks := strings.Fields(parents.Stdout)
	base := "-"
	if len(toks) >= 2 {
		base = toks[1]
	}
	fmt.Fprintf(stdio.Out, "id: %s\nref: %s\ncommit: %s\nbase: %s\n\n", id, ref, commit, base)

	showRes, err := gr.RunGit(ctx, nil, "show", "--no-patch", "--pretty=fuller", commit)
	if err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 1
	}
	fmt.Fprint(stdio.Out, showRes.Stdout)
	return 0
}

func cmdDiff(ctx context.Context, gr gitx.Runner, argv []string, stdio IO) int {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	fs.SetOutput(stdio.Err)
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stdio.Err, "diff: expected <id>")
		return 2
	}
	if err := ensureRepo(ctx, gr); err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 2
	}
	id := fs.Arg(0)
	commit, err := resolveCommit(ctx, gr, vwt.PatchRef(id))
	if err != nil {
		fmt.Fprintf(stdio.Err, "diff: unknown patch id: %s\n", id)
		return 1
	}
	base, err := commitParent(ctx, gr, commit)
	if err != nil {
		fmt.Fprintf(stdio.Err, "diff: %v\n", err)
		return 1
	}
	diffRes, err := gr.RunGit(ctx, nil, "diff",
		"--no-color",
		"--binary",
		"--full-index",
		"--no-renames",
		"--no-ext-diff",
		"--no-textconv",
		base, commit,
	)
	if err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 1
	}
	fmt.Fprint(stdio.Out, diffRes.Stdout)
	return 0
}

func cmdExport(ctx context.Context, gr gitx.Runner, argv []string, stdio IO) int {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.SetOutput(stdio.Err)
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stdio.Err, "export: expected <id>")
		return 2
	}
	if err := ensureRepo(ctx, gr); err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 2
	}
	id := fs.Arg(0)
	commit, err := resolveCommit(ctx, gr, vwt.PatchRef(id))
	if err != nil {
		fmt.Fprintf(stdio.Err, "export: unknown patch id: %s\n", id)
		return 1
	}
	base, err := commitParent(ctx, gr, commit)
	if err != nil {
		fmt.Fprintf(stdio.Err, "export: %v\n", err)
		return 1
	}
	patchRes, err := gr.RunGit(ctx, nil, "format-patch", "--stdout", base+".."+commit)
	if err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 1
	}
	fmt.Fprint(stdio.Out, patchRes.Stdout)
	return 0
}

func cmdCat(ctx context.Context, gr gitx.Runner, argv []string, stdio IO) int {
	fs := flag.NewFlagSet("cat", flag.ContinueOnError)
	fs.SetOutput(stdio.Err)
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if fs.NArg() != 1 && fs.NArg() != 2 {
		fmt.Fprintln(stdio.Err, "cat: expected <path> OR <id|rev> <path>")
		return 2
	}
	if err := ensureRepo(ctx, gr); err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 2
	}

	revOrID := "HEAD"
	path := ""
	if fs.NArg() == 1 {
		path = strings.TrimSpace(fs.Arg(0))
	} else {
		revOrID = strings.TrimSpace(fs.Arg(0))
		path = strings.TrimSpace(fs.Arg(1))
	}
	if revOrID == "" || path == "" {
		fmt.Fprintln(stdio.Err, "cat: expected <path> OR <id|rev> <path>")
		return 2
	}
	if err := validateTreePath(path); err != nil {
		fmt.Fprintf(stdio.Err, "cat: %v\n", err)
		return 2
	}

	commit, err := resolveCommitFromIDOrRev(ctx, gr, revOrID)
	if err != nil {
		fmt.Fprintf(stdio.Err, "cat: resolve commit: %v\n", err)
		return 1
	}

	res, err := gr.RunGit(ctx, nil, "show", commit+":"+path)
	if err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 1
	}
	fmt.Fprint(stdio.Out, res.Stdout)
	return 0
}

func cmdApply(ctx context.Context, gr gitx.Runner, argv []string, stdio IO) int {
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	fs.SetOutput(stdio.Err)
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stdio.Err, "apply: expected <id>")
		return 2
	}
	if err := ensureRepo(ctx, gr); err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 2
	}
	id := fs.Arg(0)
	commit, err := resolveCommit(ctx, gr, vwt.PatchRef(id))
	if err != nil {
		fmt.Fprintf(stdio.Err, "apply: unknown patch id: %s\n", id)
		return 1
	}
	base, err := commitParent(ctx, gr, commit)
	if err != nil {
		fmt.Fprintf(stdio.Err, "apply: %v\n", err)
		return 1
	}

	diffRes, err := gr.RunGit(ctx, nil, "diff",
		"--no-color",
		"--binary",
		"--full-index",
		"--no-renames",
		"--no-ext-diff",
		"--no-textconv",
		base, commit,
	)
	if err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 1
	}
	if strings.TrimSpace(diffRes.Stdout) == "" {
		return 0
	}

	if _, err := gr.RunGit(ctx, strings.NewReader(diffRes.Stdout), "apply", "--whitespace=nowarn", "--recount"); err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 1
	}
	return 0
}

func cmdDrop(ctx context.Context, gr gitx.Runner, argv []string, stdio IO) int {
	fs := flag.NewFlagSet("drop", flag.ContinueOnError)
	fs.SetOutput(stdio.Err)
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stdio.Err, "drop: expected <id>")
		return 2
	}
	if err := ensureRepo(ctx, gr); err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 2
	}
	id := fs.Arg(0)
	ref := vwt.PatchRef(id)
	commit, err := resolveCommit(ctx, gr, ref)
	if err != nil {
		fmt.Fprintf(stdio.Err, "drop: unknown patch id: %s\n", id)
		return 1
	}
	// Guarded delete: only delete if ref still points at the commit we resolved.
	if _, err := gr.RunGit(ctx, nil, "update-ref", "-d", "-m", "vwt drop "+id, ref, commit); err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 1
	}
	return 0
}

func cmdSnapshot(ctx context.Context, gr gitx.Runner, argv []string, stdio IO) int {
	fs := flag.NewFlagSet("snapshot", flag.ContinueOnError)
	fs.SetOutput(stdio.Err)
	msgFlag := fs.String("m", "", "Snapshot message")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stdio.Err, "snapshot: no arguments")
		return 2
	}
	if err := ensureRepo(ctx, gr); err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 2
	}

	snapID, err := vwtGenerateID(time.Now())
	if err != nil {
		fmt.Fprintf(stdio.Err, "snapshot: generate id: %v\n", err)
		return 1
	}
	ref := vwt.SnapshotRef(snapID)
	if err := checkRefFormat(ctx, gr, ref); err != nil {
		fmt.Fprintf(stdio.Err, "snapshot: invalid ref %q: %v\n", ref, err)
		return 2
	}
	exists, err := refExists(ctx, gr, ref)
	if err != nil {
		fmt.Fprintf(stdio.Err, "snapshot: check existing ref: %v\n", err)
		return 1
	}
	if exists {
		fmt.Fprintf(stdio.Err, "snapshot: id already exists: %s\n", snapID)
		return 1
	}

	headCommit, headErr := resolveCommit(ctx, gr, "HEAD")
	headTree := ""
	if headErr == nil {
		if t, err := resolveTree(ctx, gr, headCommit); err == nil {
			headTree = t
		}
	}

	tmpDir, err := osMkdirTemp("", "vwt-snapshot-")
	if err != nil {
		fmt.Fprintf(stdio.Err, "snapshot: temp dir: %v\n", err)
		return 1
	}
	defer osRemoveAll(tmpDir)
	idxPath := filepath.Join(tmpDir, "index")
	msgPath := filepath.Join(tmpDir, "msg")
	if err := osWriteFile(idxPath, nil, 0o600); err != nil {
		fmt.Fprintf(stdio.Err, "snapshot: temp index: %v\n", err)
		return 1
	}

	tmpGit := gr.WithEnv(map[string]string{"GIT_INDEX_FILE": idxPath})
	if headErr == nil {
		if _, err := tmpGit.RunGit(ctx, nil, "read-tree", headCommit); err != nil {
			fmt.Fprintf(stdio.Err, "snapshot: git read-tree: %v\n", err)
			return 1
		}
	} else {
		if _, err := tmpGit.RunGit(ctx, nil, "read-tree", "--empty"); err != nil {
			fmt.Fprintf(stdio.Err, "snapshot: git read-tree --empty: %v\n", err)
			return 1
		}
	}

	// Include untracked by default, exclude ignored by default.
	if _, err := tmpGit.RunGit(ctx, nil, "add", "-A"); err != nil {
		fmt.Fprintf(stdio.Err, "snapshot: git add -A: %v\n", err)
		return 1
	}
	treeRes, err := tmpGit.RunGit(ctx, nil, "write-tree")
	if err != nil {
		fmt.Fprintf(stdio.Err, "snapshot: git write-tree: %v\n", err)
		return 1
	}
	newTree := strings.TrimSpace(treeRes.Stdout)
	if headTree != "" && newTree == headTree {
		// Still produce a snapshot commit; users may want a stable base even when clean.
	}

	now := time.Now().UTC()
	subject := "vwt snapshot: " + snapID
	if strings.TrimSpace(*msgFlag) != "" {
		subject = "vwt snapshot: " + strings.TrimSpace(*msgFlag)
	}
	body := buildKV(map[string]string{
		"vwt-id":           snapID,
		"vwt-snapshot":     "true",
		"vwt-head":         headCommit,
		"vwt-created-at":   now.Format(time.RFC3339),
		"vwt-tool":         "git-vwt",
		"vwt-tool-version": "(dev)",
	})
	msg := subject + "\n\n" + body + "\n"
	if err := osWriteFile(msgPath, []byte(msg), 0o600); err != nil {
		fmt.Fprintf(stdio.Err, "snapshot: write message: %v\n", err)
		return 1
	}

	commitArgs := []string{"commit-tree", newTree, "-F", msgPath}
	if headErr == nil {
		commitArgs = append(commitArgs, "-p", headCommit)
	}
	commitGit := tmpGit.WithEnv(map[string]string{
		"GIT_AUTHOR_NAME":     "vwt",
		"GIT_AUTHOR_EMAIL":    "vwt@local",
		"GIT_COMMITTER_NAME":  "vwt",
		"GIT_COMMITTER_EMAIL": "vwt@local",
		"GIT_AUTHOR_DATE":     now.Format(time.RFC3339),
		"GIT_COMMITTER_DATE":  now.Format(time.RFC3339),
	})
	commitRes, err := commitGit.RunGit(ctx, nil, commitArgs...)
	if err != nil {
		fmt.Fprintf(stdio.Err, "snapshot: git commit-tree: %v\n", err)
		return 1
	}
	commit := strings.TrimSpace(commitRes.Stdout)
	// Create ref atomically (no clobber) by requiring an all-zero old value.
	if _, err := gr.RunGit(ctx, nil, "update-ref", "-m", "vwt snapshot "+snapID, ref, commit, zeroOID); err != nil {
		if exists, exErr := refExists(ctx, gr, ref); exErr == nil && exists {
			fmt.Fprintf(stdio.Err, "snapshot: id already exists: %s\n", snapID)
			return 1
		}
		fmt.Fprintf(stdio.Err, "snapshot: git update-ref: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdio.Out, "%s\t%s\t%s\n", snapID, commit, headCommit)
	return 0
}

func cmdGC(ctx context.Context, gr gitx.Runner, argv []string, stdio IO) int {
	fs := flag.NewFlagSet("gc", flag.ContinueOnError)
	fs.SetOutput(stdio.Err)
	keepDays := fs.Int("keep-days", 0, "Delete patches older than N days (0 = keep all)")
	dryRun := fs.Bool("dry-run", false, "Print what would be deleted")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if err := ensureRepo(ctx, gr); err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 2
	}
	if *keepDays <= 0 {
		// No-op by default.
		return 0
	}

	// Best-effort: delete patch refs with committerdate older than keep-days.
	cutoff := time.Now().Add(-time.Duration(*keepDays) * 24 * time.Hour)
	res, err := gr.RunGit(ctx, nil, "for-each-ref", vwt.PatchRefPrefix, "--format=%(refname)\t%(objectname)\t%(committerdate:unix)")
	if err != nil {
		fmt.Fprintln(stdio.Err, err)
		return 1
	}
	out := strings.TrimSpace(res.Stdout)
	if out == "" {
		return 0
	}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		ref := parts[0]
		oid := parts[1]
		unixStr := parts[2]
		sec, err := parseInt64(unixStr)
		if err != nil {
			continue
		}
		t := time.Unix(sec, 0)
		if t.Before(cutoff) {
			if *dryRun {
				fmt.Fprintf(stdio.Out, "would drop %s\n", ref)
				continue
			}
			// Guarded delete: only delete if ref still points at the oid we listed.
			if _, err := gr.RunGit(ctx, nil, "update-ref", "-d", "-m", "vwt gc", ref, oid); err != nil {
				fmt.Fprintf(stdio.Err, "gc: drop %s: %v\n", ref, err)
				return 1
			}
		}
	}
	return 0
}

func commitParent(ctx context.Context, gr gitx.Runner, commit string) (string, error) {
	res, err := gr.RunGit(ctx, nil, "rev-list", "--parents", "-n", "1", commit)
	if err != nil {
		return "", err
	}
	toks := strings.Fields(res.Stdout)
	if len(toks) < 2 {
		return "", fmt.Errorf("commit has no parent (expected base parent): %s", commit)
	}
	return toks[1], nil
}

func resolveCommitFromIDOrRev(ctx context.Context, gr gitx.Runner, idOrRev string) (string, error) {
	idOrRev = strings.TrimSpace(idOrRev)
	if idOrRev == "" {
		return "", errors.New("empty")
	}
	if c, err := resolveCommit(ctx, gr, vwt.PatchRef(idOrRev)); err == nil {
		return c, nil
	}
	if c, err := resolveCommit(ctx, gr, vwt.SnapshotRef(idOrRev)); err == nil {
		return c, nil
	}
	return resolveCommit(ctx, gr, idOrRev)
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

func shortSHA(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

func buildKV(kv map[string]string) string {
	// Keep stable order.
	keys := make([]string, 0, len(kv))
	for k := range kv {
		keys = append(keys, k)
	}
	sortStrings(keys)
	var b strings.Builder
	for _, k := range keys {
		v := strings.TrimSpace(kv[k])
		if v == "" {
			continue
		}
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(v)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func sortStrings(ss []string) {
	// Tiny local insertion sort to avoid importing sort in main.
	for i := 1; i < len(ss); i++ {
		j := i
		for j > 0 && ss[j-1] > ss[j] {
			ss[j-1], ss[j] = ss[j], ss[j-1]
			j--
		}
	}
}

func parseInt64(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("empty")
	}
	var n int64
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("not a number: %q", s)
		}
		n = n*10 + int64(c-'0')
	}
	return n, nil
}
