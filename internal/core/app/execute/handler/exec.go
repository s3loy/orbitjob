package handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"

	"orbitjob/internal/core/app/execute"
)

const maxStderrBytes = 4096

type ExecHandler struct{}

func (h *ExecHandler) Execute(ctx context.Context, task execute.AssignedTask) execute.Result {
	command, args, env, err := parseExecPayload(task.HandlerPayload)
	if err != nil {
		return execute.Result{
			Success:    false,
			ResultCode: "invalid_payload",
			ErrorMsg:   err.Error(),
		}
	}

	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Env = mergeEnv(os.Environ(), env)

	var stderr bytes.Buffer
	cmd.Stderr = &limitedWriter{buf: &stderr, limit: maxStderrBytes}
	out, runErr := cmd.Output()
	_ = out

	if runErr == nil {
		return execute.Result{
			Success:    true,
			ResultCode: "0",
		}
	}

	if ctx.Err() != nil {
		return execute.Result{
			Success:    false,
			ResultCode: "timeout",
			ErrorMsg:   fmt.Sprintf("execution timed out: %v", ctx.Err()),
		}
	}

	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return execute.Result{
			Success:    false,
			ResultCode: strconv.Itoa(exitErr.ExitCode()),
			ErrorMsg:   truncate(stderr.String(), maxStderrBytes),
		}
	}

	return execute.Result{
		Success:    false,
		ResultCode: "error",
		ErrorMsg:   runErr.Error(),
	}
}

func parseExecPayload(p map[string]any) (command string, args []string, env map[string]string, err error) {
	cmdRaw, ok := p["command"]
	if !ok {
		return "", nil, nil, fmt.Errorf("missing required field: command")
	}
	command, ok = cmdRaw.(string)
	if !ok || command == "" {
		return "", nil, nil, fmt.Errorf("command must be a non-empty string")
	}

	if argsRaw, ok := p["args"]; ok {
		argsSlice, ok := argsRaw.([]any)
		if !ok {
			return "", nil, nil, fmt.Errorf("args must be an array of strings")
		}
		for i, a := range argsSlice {
			s, ok := a.(string)
			if !ok {
				return "", nil, nil, fmt.Errorf("args[%d] must be a string", i)
			}
			args = append(args, s)
		}
	}

	if envRaw, ok := p["env"]; ok {
		envMap, ok := envRaw.(map[string]any)
		if !ok {
			return "", nil, nil, fmt.Errorf("env must be a map of string to string")
		}
		env = make(map[string]string, len(envMap))
		for k, v := range envMap {
			s, ok := v.(string)
			if !ok {
				return "", nil, nil, fmt.Errorf("env[%q] must be a string", k)
			}
			env[k] = s
		}
	}

	return command, args, env, nil
}

func mergeEnv(base []string, extra map[string]string) []string {
	if len(extra) == 0 {
		return base
	}
	merged := make([]string, len(base), len(base)+len(extra))
	copy(merged, base)
	for k, v := range extra {
		merged = append(merged, k+"="+v)
	}
	return merged
}

type limitedWriter struct {
	buf   *bytes.Buffer
	limit int
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	remaining := w.limit - w.buf.Len()
	if remaining <= 0 {
		return len(p), nil
	}
	if len(p) > remaining {
		p = p[:remaining]
	}
	return w.buf.Write(p)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
