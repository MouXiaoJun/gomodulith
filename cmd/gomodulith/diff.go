package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/MouXiaoJun/gomodulith/modulith"
)

// runDiff implements `gomodulith diff <base> <head>`. Each argument is either
// a path to an exported architecture JSON file or a git revision (e.g. HEAD,
// HEAD~1, a tag or a commit hash) that is checked out to a temporary
// directory and scanned.
func runDiff(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("diff", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(stderr, "Usage: gomodulith diff <base> <head>\n")
		fmt.Fprint(stderr, "\nCompares two architecture states and reports the changes between them.\n")
		fmt.Fprint(stderr, "Each of <base> and <head> is either a path to an architecture JSON file\n")
		fmt.Fprint(stderr, "(produced by `gomodulith export`) or a git revision such as HEAD, HEAD~1,\n")
		fmt.Fprint(stderr, "a tag or a commit hash, which is scanned from a temporary checkout.\n")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	pos := fs.Args()
	if len(pos) != 2 {
		fmt.Fprintln(stderr, "gomodulith diff: exactly two arguments are required (<base> <head>)")
		fs.Usage()
		return 2
	}

	ctx := context.Background()
	base, err := modelFromArg(ctx, pos[0])
	if err != nil {
		fmt.Fprintf(stderr, "gomodulith diff: base: %v\n", err)
		return 1
	}
	head, err := modelFromArg(ctx, pos[1])
	if err != nil {
		fmt.Fprintf(stderr, "gomodulith diff: head: %v\n", err)
		return 1
	}

	changes := modulith.Diff(base, head)
	if len(changes) == 0 {
		fmt.Fprintln(stdout, "no architecture changes")
		return 0
	}
	for _, c := range changes {
		fmt.Fprintln(stdout, c.String())
	}
	return 0
}

// modelFromArg resolves an argument to an ArchitectureModel: an existing file
// is read as an exported JSON model, anything else is treated as a git
// revision and scanned from a temporary checkout.
func modelFromArg(ctx context.Context, arg string) (*modulith.ArchitectureModel, error) {
	if st, err := os.Stat(arg); err == nil && !st.IsDir() {
		return readModel(arg)
	}
	model, err := modelFromGitRef(ctx, arg)
	if err != nil {
		return nil, fmt.Errorf("cannot read %q as a file or a git revision: %w", arg, err)
	}
	return model, nil
}

// modelFromGitRef scans the repository at the given git revision from a
// temporary worktree and returns its architecture model.
func modelFromGitRef(ctx context.Context, ref string) (*modulith.ArchitectureModel, error) {
	tmp, err := os.MkdirTemp("", "gomodulith-ref-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	if err := extractGitRef(ctx, ref, tmp); err != nil {
		return nil, err
	}

	app, err := loadAppIn(ctx, tmp, nil, false)
	if err != nil {
		return nil, fmt.Errorf("scan git revision %q: %w", ref, err)
	}
	model, err := app.Model(false)
	if err != nil {
		return nil, err
	}
	return model, nil
}

// extractGitRef checks out ref into dst using `git archive` piped through tar,
// which is read-only and does not touch the working tree or git metadata.
func extractGitRef(ctx context.Context, ref, dst string) error {
	arch := exec.CommandContext(ctx, "git", "archive", "--format=tar", ref)
	tar := exec.CommandContext(ctx, "tar", "-xf", "-", "-C", dst)
	pipe, err := arch.StdoutPipe()
	if err != nil {
		return err
	}
	tar.Stdin = pipe
	if err := tar.Start(); err != nil {
		return err
	}
	// arch.Run closes the pipe when it finishes; tar.Wait must come after.
	if err := arch.Run(); err != nil {
		return fmt.Errorf("git archive %s: %w", ref, err)
	}
	if err := tar.Wait(); err != nil {
		return fmt.Errorf("extract archive: %w", err)
	}
	return nil
}

func readModel(path string) (*modulith.ArchitectureModel, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var m modulith.ArchitectureModel
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse %s as architecture JSON: %w", path, err)
	}
	return &m, nil
}
