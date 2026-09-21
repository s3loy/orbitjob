package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// printTable renders a fixed-width, greppable table. Headers are uppercase;
// every row is one line and columns never bleed into each other, so both
// eyes and grep can read the output.
func printTable(headers []string, rows [][]string) {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	var out strings.Builder
	for i, h := range headers {
		out.WriteString(pad(h, widths[i]))
		out.WriteString("  ")
	}
	line := strings.TrimRight(out.String(), " ")
	_, _ = fmt.Fprintln(stdoutWriter, line)

	separator := strings.Builder{}
	for i := range headers {
		separator.WriteString(strings.Repeat("-", widths[i]))
		separator.WriteString("  ")
	}
	_, _ = fmt.Fprintln(stdoutWriter, strings.TrimRight(separator.String(), " "))

	for _, row := range rows {
		out.Reset()
		for i := range headers {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			out.WriteString(pad(cell, widths[i]))
			out.WriteString("  ")
		}
		_, _ = fmt.Fprintln(stdoutWriter, strings.TrimRight(out.String(), " "))
	}
}

func pad(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

// printDetail renders a single object as label: value lines. Greppable the
// same way a table is, and stable in the face of long values.
func printDetail(pairs []detailField) {
	for _, p := range pairs {
		_, _ = fmt.Fprintf(stdoutWriter, "%s: %s\n", p.label, p.value)
	}
}

type detailField struct {
	label string
	value string
}

func field(label, value string) detailField {
	return detailField{label: label, value: value}
}

func fieldTime(label, value string) detailField {
	if value == "" {
		value = "-"
	}
	return field(label, value)
}

func fieldPtr(label string, value *string) detailField {
	if value == nil || *value == "" {
		return field(label, "-")
	}
	return field(label, *value)
}

// emitJSON prints one value as indented JSON for --json mode on paths that do
// not proxy a raw API body (status, doctor).
func emitJSON(v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fatal("encode JSON output: %v", err)
	}
	_, _ = fmt.Fprintln(stdoutWriter, string(data))
	return nil
}
