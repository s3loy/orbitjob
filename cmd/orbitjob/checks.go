package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
)

const checksUsage = "orbitjob checks <list|get> [options]"

func checksCommand(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usageFail(checksUsage)
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "list":
		return checksList(ctx, rest)
	case "get":
		return checksGet(ctx, rest)
	default:
		return usageFail(checksUsage)
	}
}

// checksList mirrors GET /api/v1/checks: the tenant's check definitions.
func checksList(ctx context.Context, args []string) error {
	usageLine := "orbitjob checks list [--status active|paused] [--limit N] [--offset N] [--api-url URL] [--api-key KEY] [--json]"
	var conn connFlags
	var status string
	var limit, offset int
	return runWithConn(ctx, args, &conn, usageLine, func(fs *flag.FlagSet) {
		fs.StringVar(&status, "status", "", "only active or paused checks")
		fs.IntVar(&limit, "limit", 0, "page size, 1-100 (default 50)")
		fs.IntVar(&offset, "offset", 0, "rows to skip")
	}, func(client *apiClient) error {
		data, err := client.do(ctx, http.MethodGet, "/api/v1/checks", map[string]string{
			"status": status,
			"limit":  intOrEmpty(limit),
			"offset": intOrEmpty(offset),
		}, nil)
		if err != nil {
			return err
		}
		return printJSONOr(client, data, func() {
			items, _ := decodeList[checkItem](data)
			if len(items) == 0 {
				_, _ = fmt.Fprintln(stdoutWriter, "no checks")
				return
			}
			rows := make([][]string, 0, len(items))
			for _, c := range items {
				rows = append(rows, []string{
					fmt.Sprintf("%d", c.ID),
					c.Name,
					c.Status,
					c.CheckType,
					c.ScheduleType,
					timeOrDash(c.NextRunAt),
					c.CreatedAt,
				})
			}
			printTable([]string{"ID", "NAME", "STATUS", "TYPE", "SCHEDULE", "NEXT_RUN", "CREATED"}, rows)
		})
	})
}

const checkIDUsage = "orbitjob checks get ID [--api-url URL] [--api-key KEY] [--json]"

func checksGet(ctx context.Context, args []string) error {
	parsed, err := parseIDCommand(args, checkIDUsage, "check id", nil)
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
	data, err := client.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/checks/%d", parsed.id), nil, nil)
	if err != nil {
		return err
	}
	return printJSONOr(client, data, func() {
		check, _ := decodeOne[checkDetail](data)
		schedule := check.ScheduleType
		if check.CronExpr != nil && *check.CronExpr != "" {
			schedule = "cron: " + *check.CronExpr
		}
		if check.IntervalSec != nil {
			schedule = fmt.Sprintf("interval: %ds", *check.IntervalSec)
		}
		printDetail([]detailField{
			field("id", fmt.Sprintf("%d", check.ID)),
			field("name", check.Name),
			field("status", check.Status),
			field("type", check.CheckType),
			field("schedule", schedule),
			field("timeout_sec", fmt.Sprintf("%d", check.TimeoutSec)),
			field("retry_limit", fmt.Sprintf("%d", check.RetryLimit)),
			field("priority", fmt.Sprintf("%d", check.Priority)),
			fieldPtr("next_run_at", check.NextRunAt),
			fieldTime("created_at", check.CreatedAt),
			fieldTime("updated_at", check.UpdatedAt),
		})
	})
}
