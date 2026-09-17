package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
)

const jobsUsage = "orbitjob jobs <list|get|trigger> [options]"

func jobsCommand(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usageFail(jobsUsage)
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "list":
		return jobsList(ctx, rest)
	case "get":
		return jobsGet(ctx, rest)
	case "trigger":
		return jobsTrigger(ctx, rest)
	default:
		return usageFail(jobsUsage)
	}
}

// jobsList mirrors GET /api/v1/jobs: the active revision of every definition
// in the caller's tenant.
func jobsList(ctx context.Context, args []string) error {
	usageLine := "orbitjob jobs list [--limit N] [--offset N] [--api-url URL] [--api-key KEY] [--json]"
	var conn connFlags
	var limit, offset int
	return runWithConn(ctx, args, &conn, usageLine, func(fs *flag.FlagSet) {
		fs.IntVar(&limit, "limit", 0, "page size, 1-100 (default 50)")
		fs.IntVar(&offset, "offset", 0, "rows to skip")
	}, func(client *apiClient) error {
		data, err := client.do(ctx, http.MethodGet, "/api/v1/jobs", map[string]string{
			"limit":  intOrEmpty(limit),
			"offset": intOrEmpty(offset),
		}, nil)
		if err != nil {
			return err
		}
		return printJSONOr(client, data, func() {
			items, _ := decodeList[jobItem](data)
			if len(items) == 0 {
				fmt.Fprintln(stdoutWriter, "no jobs")
				return
			}
			rows := make([][]string, 0, len(items))
			for _, j := range items {
				rows = append(rows, []string{
					fmt.Sprintf("%d", j.ID),
					j.Name,
					j.Namespace,
					j.ScheduleSummary,
					j.CreatedAt,
				})
			}
			printTable([]string{"ID", "NAME", "NAMESPACE", "SCHEDULE", "CREATED"}, rows)
		})
	})
}

const jobIDUsage = "orbitjob jobs get ID [--api-url URL] [--api-key KEY] [--json]"

func jobsGet(ctx context.Context, args []string) error {
	parsed, err := parseIDCommand(args, jobIDUsage, "job id", nil)
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
	data, err := client.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/jobs/%d", parsed.id), nil, nil)
	if err != nil {
		return err
	}
	return printJSONOr(client, data, func() {
		job, _ := decodeOne[jobDetail](data)
		printDetail([]detailField{
			field("id", fmt.Sprintf("%d", job.ID)),
			field("name", job.Name),
			field("namespace", job.Namespace),
			field("schedule", job.ScheduleSummary),
			fieldBool("suspend", job.Suspend),
			field("timeout_seconds", fmt.Sprintf("%d", job.TimeoutSeconds)),
			field("retry_max_attempts", fmt.Sprintf("%d", job.RetryMaxAttempts)),
			field("image", job.JobTemplate.Image),
			field("actor", job.Actor),
			fieldTime("created_at", job.CreatedAt),
		})
	})
}

func fieldBool(label string, v bool) detailField {
	if v {
		return field(label, "true")
	}
	return field(label, "false")
}

const jobTriggerUsage = "orbitjob jobs trigger ID [--idempotency-key KEY] [--api-url URL] [--api-key KEY] [--json]"

// jobsTrigger publishes a manual run through the real trigger route. An
// idempotency key makes a retry return the same run instead of a second one.
func jobsTrigger(ctx context.Context, args []string) error {
	var idemKey string
	parsed, err := parseIDCommand(args, jobTriggerUsage, "job id", func(fs *flag.FlagSet) {
		fs.StringVar(&idemKey, "idempotency-key", "", "retry-safe key; the same key returns the same run")
	})
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
	data, err := client.doWithHeader(ctx, http.MethodPost,
		fmt.Sprintf("/api/v1/jobs/%d/trigger", parsed.id),
		map[string]string{"X-OrbitJob-Idempotency-Key": idemKey},
		nil, nil)
	if err != nil {
		return err
	}
	return printJSONOr(client, data, func() {
		ref, _ := decodeOne[runRef](data)
		fmt.Fprintf(stdoutWriter,
			"[OK] triggered job %d (jobrun %s/%s, occurrence %s, phase %s)\n",
			parsed.id, ref.Namespace, ref.Name, ref.OccurrenceKey, ref.Phase)
	})
}
