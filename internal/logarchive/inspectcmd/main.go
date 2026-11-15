// Command inspectcmd checks a stopped archive or isolated snapshot without writes.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"orbitjob/internal/logarchive"
)

func main() {
	directory := flag.String("directory", "", "stopped archive root or isolated snapshot")
	budget := flag.Duration("timeout", time.Minute, "maximum inspection duration")
	flag.Parse()
	if *directory == "" || *budget <= 0 {
		fmt.Fprintln(os.Stderr, "--directory and a positive --timeout are required")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *budget)
	defer cancel()
	report, err := logarchive.Inspect(ctx, *directory, logarchive.DefaultLimits())
	_ = json.NewEncoder(os.Stdout).Encode(report)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if len(report.Findings) > 0 {
		os.Exit(1)
	}
}
