// Command validate runs the test suite against the database and writes a
// stamped traceability report (SPEC-004). The report is test evidence only, not
// a validation package. See specs/implemented/SPEC-004-traceability-harness/design.md.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func main() {
	code, err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "validate:", err)
		os.Exit(2)
	}
	os.Exit(code)
}

// run executes the six-step run sequence and returns the exit code: 1 when the
// result is FAIL, 0 when it is PASS. Errors are returned, not printed.
func run() (int, error) {
	dbURL := os.Getenv("LDE_TEST_DATABASE_URL")
	if dbURL == "" {
		return 2, errors.New("LDE_TEST_DATABASE_URL is not set")
	}

	operator := flag.String("operator", "", "name of the person running the validation (required)")
	root := flag.String("root", ".", "repository root")
	out := flag.String("out", "validation", "output directory, relative to the current directory")
	flag.Parse()
	if *operator == "" {
		return 2, errors.New("--operator is required")
	}

	// Truncated to whole seconds so started_at matches the folder stamp.
	startedAt := time.Now().UTC().Truncate(time.Second)

	commit, err := gitOutput(*root, "rev-parse", "HEAD")
	if err != nil {
		return 2, err
	}
	status, err := gitOutput(*root, "status", "--porcelain")
	if err != nil {
		return 2, err
	}
	dirty := status != ""

	reqs, err := parseSpecs(filepath.Join(*root, "specs"))
	if err != nil {
		return 2, err
	}
	module, err := readModule(*root)
	if err != nil {
		return 2, err
	}
	cites, err := scanTests(*root, module)
	if err != nil {
		return 2, err
	}
	results, goTestPassed, err := goTest(*root)
	if err != nil {
		return 2, err
	}
	finishedAt := time.Now().UTC().Truncate(time.Second)

	// After the tests, which migrate the database.
	pgVersion, migrations, err := readDatabase(dbURL)
	if err != nil {
		return 2, err
	}

	rep := buildReport(reqs, cites, results, goTestPassed, dirty)
	rep.Environment = environment{
		Operator:        *operator,
		GitCommit:       commit,
		GitDirty:        dirty,
		GoVersion:       runtime.Version(),
		OSArch:          runtime.GOOS + "/" + runtime.GOARCH,
		PostgresVersion: pgVersion,
		Migrations:      migrations,
		StartedAt:       startedAt,
		FinishedAt:      finishedAt,
	}

	dir := filepath.Join(*out, startedAt.Format("20060102T150405Z"))
	sha, err := writeReport(dir, rep, module)
	if err != nil {
		return 2, err
	}

	fmt.Printf("report: %s\n", dir)
	fmt.Printf("report.json SHA-256: %s\n", sha)
	fmt.Printf("verified %d, not verified %d, total %d\n",
		rep.Summary.Verified, rep.Summary.NotVerified, rep.Summary.Total)
	fmt.Printf("Result: %s\n", rep.Result)
	if rep.Result == "FAIL" {
		return 1, nil
	}
	return 0, nil
}

// gitOutput runs git in root and returns its trimmed standard output.
func gitOutput(root string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// readDatabase returns the server version and the applied migration versions.
func readDatabase(dbURL string) (string, []string, error) {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		return "", nil, err
	}
	defer conn.Close(ctx)

	var version string
	if err := conn.QueryRow(ctx, "SHOW server_version").Scan(&version); err != nil {
		return "", nil, err
	}
	rows, err := conn.Query(ctx, "SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		return "", nil, err
	}
	migrations, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return "", nil, err
	}
	return version, migrations, nil
}
