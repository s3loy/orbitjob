package handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"orbitjob/internal/core/app/execute"
)

const maxStderrBytes = 4096

// envWhitelist defines environment variables inherited from the parent process.
var envWhitelist = map[string]bool{
	"PATH":   true,
	"TMPDIR": true,
	"TEMP":   true,
	"TMP":    true,
	"LANG":   true,
}

// envBlacklist defines environment variables forbidden in user-declared env.
// DYLD_* prefix is also blocked (see isBlacklisted).
var envBlacklist = map[string]bool{
	"LD_PRELOAD":      true,
	"LD_LIBRARY_PATH": true,
	"PYTHONPATH":      true,
	"PERL5LIB":        true,
	"RUBYLIB":         true,
}

func isBlacklisted(k string) bool {
	if envBlacklist[k] {
		return true
	}
	return strings.HasPrefix(k, "DYLD_")
}

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
	cmd.Env = buildEnv(env)

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

func buildEnv(userEnv map[string]string) []string {
	env := []string{
		"HOME=/tmp",
		"USER=nobody",
	}
	for k := range envWhitelist {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	for k, v := range userEnv {
		if isBlacklisted(k) {
			continue
		}
		env = append(env, k+"="+v)
	}
	return env
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
