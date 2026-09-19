// Command orbitjob is the developer CLI for an OrbitJob installation. It is a
// client only: it talks to the admin API over HTTP and to the cluster through
// kubectl, and it never opens a database connection.
package main

import (
	"context"
	"fmt"
	"os"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:]))
}

func run(ctx context.Context, args []string) int {
	if len(args) == 0 {
		usage(os.Stderr)
		return 1
	}
	name, rest := args[0], args[1:]
	cmd, ok := commands[name]
	if !ok {
		fmt.Fprintf(os.Stderr, "orbitjob: unknown command %q\n\n", name)
		usage(os.Stderr)
		return 1
	}
	if err := cmd(ctx, rest); err != nil {
		return exitCodeOf(err)
	}
	return 0
}

// exitCodeOf maps a command failure to the CLI's exit contract. Typed API
// failures keep their codes (3 not found, 4 forbidden); everything else,
// including usage errors, exits 1. A -h that flag already answered is success.
func exitCodeOf(err error) int {
	if e, ok := err.(*exitError); ok {
		if e.message != "" {
			fmt.Fprintln(os.Stderr, e.message)
		}
		return e.code
	}
	if apiErr, ok := err.(*apiError); ok {
		fmt.Fprintf(os.Stderr, "orbitjob: %v\n", apiErr)
		return apiErr.exitCode()
	}
	fmt.Fprintf(os.Stderr, "orbitjob: %v\n", err)
	return 1
}
