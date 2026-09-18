package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
)

const runsUsage = "orbitjob runs <list|get|cancel> [options]"

func runsCommand(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usageFail(runsUsage)
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "list":
		return runsList(ctx, rest)
	case "get":
		return runsGet(ctx, rest)
	case "cancel":
		return runsCancel(ctx, rest)
	default:
		return usageFail(runsUsage)
	}
}

// runsList mirrors GET /api/v1/instances: phase, limit and offset, with the
// server deciding the tenant from the key.
func runsList(ctx context.Context, args []string) error {
	usageLine := "orbitjob runs list [--phase PHASE] [--limit N] [--offset N] [--api-url URL] [--api-key KEY] [--json]"
	var conn connFlags
	var phase string
	var limit, offset int
	return runWithConn(ctx, args, &conn, usageLine, func(fs *flag.FlagSet) {
		fs.StringVar(&phase, "phase", "", "only this run phase (e.g. Running, Succeeded, Failed)")
		fs.IntVar(&limit, "limit", 0, "page size, 1-100 (default 50)")
		fs.IntVar(&offset, "offset", 0, "rows to skip")
	}, func(client *apiClient) error {
		data, err := client.do(ctx, http.MethodGet, "/api/v1/instances", map[string]string{
			"phase":  phase,
			"limit":  intOrEmpty(limit),
			"offset": intOrEmpty(offset),
		}, nil)
		if err != nil {
			return err
		}
		return printJSONOr(client, data, func() {
			items, _ := decodeList[runItem](data)
			if len(items) == 0 {
				_, _ = fmt.Fprintln(stdoutWriter, "no runs")
				return
			}
			rows := make([][]string, 0, len(items))
			for _, r := range items {
				rows = append(rows, []string{
					fmt.Sprintf("%d", r.ID),
					r.Phase,
					fmt.Sprintf("%d/%d", r.Attempt, r.MaxAttempts),
					r.Trigger,
					r.OccurrenceKey,
					r.UpdatedAt,
				})
			}
			printTable([]string{"ID", "PHASE", "ATTEMPTS", "TRIGGER", "OCCURRENCE_KEY", "UPDATED"}, rows)
		})
	})
}

func intOrEmpty(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("%d", n)
}

// runWithConn is the shared body of an API command: parse the shared
// connection flags plus the command's own, resolve the client, run.
func runWithConn(ctx context.Context, args []string, conn *connFlags, usageLine string, extra func(*flag.FlagSet), body func(*apiClient) error) error {
	fs := flagSet(usageLine)
	conn.add(fs)
	if extra != nil {
		extra(fs)
	}
	help, err := parseArgs(fs, args, usageLine)
	if err != nil {
		return err
	}
	if help {
		return nil
	}
	client, err := conn.resolve()
	if err != nil {
		return err
	}
	return body(client)
}

const runIDUsage = "orbitjob runs get ID [--api-url URL] [--api-key KEY] [--json]"

func runsGet(ctx context.Context, args []string) error {
	parsed, err := parseIDCommand(args, runIDUsage, "run id", nil)
	if err != nil {
		return err
	}
	if parsed.done {
		return nil
	}
	client, err := resolveClient(parsed.conn)
	if err != nil {
		return err
	}
	return runsGetBody(ctx, client, parsed.id)
}

func runsGetBody(ctx context.Context, client *apiClient, id int64) error {
	data, err := client.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/instances/%d", id), nil, nil)
	if err != nil {
		return err
	}
	return printJSONOr(client, data, func() {
		run, _ := decodeOne[runDetail](data)
		printDetail([]detailField{
			field("id", fmt.Sprintf("%d", run.ID)),
			field("phase", run.Phase),
			field("attempts", fmt.Sprintf("%d/%d", run.Attempt, run.MaxAttempts)),
			field("occurrence_key", run.OccurrenceKey),
			field("trigger", run.Trigger),
			field("actor", run.Actor),
			fieldTime("created_at", run.CreatedAt),
			fieldTime("updated_at", run.UpdatedAt),
		})
		if len(run.Attempts) > 0 {
			_, _ = fmt.Fprintln(stdoutWriter)
			rows := make([][]string, 0, len(run.Attempts))
			for _, a := range run.Attempts {
				rows = append(rows, []string{
					fmt.Sprintf("%d", a.AttemptNumber),
					a.Phase,
					a.KubernetesJobName,
					timeOrDash(a.StartedAt),
					timeOrDash(a.CompletedAt),
				})
			}
			printTable([]string{"ATTEMPT", "PHASE", "K8S_JOB", "STARTED", "COMPLETED"}, rows)
		}
	})
}

func timeOrDash(v *string) string {
	if v == nil || *v == "" {
		return "-"
	}
	return *v
}

const runCancelUsage = "orbitjob runs cancel ID [--api-url URL] [--api-key KEY] [--json]"

// runsCancel posts the real cancel route. The response carries the phase at
// request time; reaching Canceled is the operator's job, so the CLI says
// "at request" rather than promising the stop.
func runsCancel(ctx context.Context, args []string) error {
	parsed, err := parseIDCommand(args, runCancelUsage, "run id", nil)
	if err != nil {
		return err
	}
	if parsed.done {
		return nil
	}
	client, err := resolveClient(parsed.conn)
	if err != nil {
		return err
	}
	data, err := client.do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/instances/%d/cancel", parsed.id), nil, nil)
	if err != nil {
		return err
	}
	return printJSONOr(client, data, func() {
		ref, _ := decodeOne[runRef](data)
		_, _ = fmt.Fprintf(stdoutWriter,
			"[OK] cancel requested for run %d (jobrun %s/%s, occurrence %s, phase at request: %s)\n",
			parsed.id, ref.Namespace, ref.Name, ref.OccurrenceKey, ref.Phase)
	})
}
