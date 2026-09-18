package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
)

// exitError carries the CLI's exit contract from a command to main. code 1 is
// the generic failure; 3 and 4 are the typed API failures (not found,
// forbidden).
type exitError struct {
	code    int
	message string
}

// Error keeps exitError inside the error world so commands can return it
// through the ordinary error path.
func (e *exitError) Error() string { return e.message }

func failf(code int, format string, a ...any) *exitError {
	return &exitError{code: code, message: fmt.Sprintf(format, a...)}
}

// command is one top-level subcommand. It parses its own flags, prints its
// output to stdout, and returns an error whose exit code main applies.
type command func(ctx context.Context, args []string) error

// commands is the dispatch table. Keep it in alphabetical order in usage.
var commands = map[string]command{
	"checks": checksCommand,
	"doctor": doctorCommand,
	"jobs":   jobsCommand,
	"runs":   runsCommand,
	"status": statusCommand,
}

const usageText = `usage: orbitjob <command> [options]

Developer CLI for an OrbitJob installation. A client only: it talks to the
admin API over HTTP and to the cluster through kubectl; it never touches the
database.

Commands:
  status          install snapshot from the cluster (helm release, pods,
                  schema version, CRDs, monitoring)
  doctor          full diagnostic bundle: API auth path, cluster RBAC, lease,
                  restart counts; exits non-zero on any [FAIL]
  runs list       list runs (GET /api/v1/instances)
  runs get ID     one run with its attempt trail
  runs cancel ID  request a stop for one run
  jobs list       list job definitions (GET /api/v1/jobs)
  jobs get ID     one definition's active revision
  jobs trigger ID publish a JobRun for a manual run
  checks list     list checks (GET /api/v1/checks)
  checks get ID   one check

Connection flags (accepted by every command):
  --api-url URL   admin API base URL (default http://localhost:8080)
  --api-key KEY   bearer key for the admin API
  --json          print the raw API response instead of a table

Environment:
  ORBITJOB_API_URL, ORBITJOB_API   admin API base URL and key, used when the
                                   flags are absent (make kind-env exports
                                   both); "doctor" also reads the key from the
                                   bootstrap-api-key secret as a last resort

Exit codes:
  0  success (doctor: no [FAIL] lines; WARN is tolerated)
  1  usage error, connection refused, server error, or any doctor [FAIL]
  3  the requested object does not exist (API 404)
  4  the key may not do that (API 403, or 401)

Run "orbitjob <command> -h" for a command's options.`

func usage(w io.Writer) {
	// A CLI whose stdout has gone away has nothing useful to do with the write
	// error; the usage text is best effort.
	_, _ = fmt.Fprintln(w, usageText)
}

// usageFail is the error a subcommand returns when argv does not name a known
// subcommand.
func usageFail(usageLine string) error {
	return failf(1, "usage: %s", usageLine)
}

// fatal wraps an unexpected command failure as exit 1 with a spoken message.
func fatal(format string, a ...any) error {
	return failf(1, format, a...)
}

// valueFlags are the options that take a separate value argument. The CLI's
// flag surface is finite, so the splitter can stay exact; "--flag=value" form
// needs no lookahead.
var valueFlags = map[string]bool{
	"--api-url":              true,
	"--api-key":              true,
	"--phase":                true,
	"--idempotency-key":      true,
	"--namespace":            true,
	"--release":              true,
	"--namespaces":           true,
	"--monitoring-namespace": true,
}

// splitPositional separates flags from positional arguments so the ID
// commands accept their id anywhere: "runs get 7 --json" and
// "runs get --api-url URL 7" both parse. A value flag consumes the argument
// after it, so a key or URL is never mistaken for the id.
func splitPositional(args []string) (positional, flagArgs []string) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") && arg != "-" {
			flagArgs = append(flagArgs, arg)
			if valueFlags[arg] && i+1 < len(args) {
				i++
				flagArgs = append(flagArgs, args[i])
			}
			continue
		}
		positional = append(positional, arg)
	}
	return positional, flagArgs
}

// idCommand is the parsed form of a command that takes one positional id.
type idCommand struct {
	conn connFlags
	id   int64
	done bool // help was requested and printed; the caller returns success
}

// parseIDCommand parses one positional id plus the shared connection flags
// (and any command-specific extras) for the ID-first commands.
func parseIDCommand(args []string, usageLine, kind string, extra func(*flag.FlagSet)) (idCommand, error) {
	positional, flagArgs := splitPositional(args)
	fs := flagSet(usageLine)
	var parsed idCommand
	parsed.conn.add(fs)
	if extra != nil {
		extra(fs)
	}
	help, err := parseArgs(fs, flagArgs, usageLine)
	if err != nil {
		return parsed, err
	}
	if help {
		parsed.done = true
		return parsed, nil
	}
	if len(positional) != 1 {
		return parsed, usageFail(usageLine)
	}
	parsed.id, err = positiveID(positional[0], kind, usageLine)
	if err != nil {
		return parsed, err
	}
	return parsed, nil
}

// resolveClient is runWithConn's tail for already-parsed commands.
func resolveClient(conn connFlags) (*apiClient, error) {
	return conn.resolve()
}
