package handler

import (
	"context"
	"runtime"
	"testing"
	"time"

	"orbitjob/internal/core/app/execute"
)

func echoCommand() string {
	if runtime.GOOS == "windows" {
		return "cmd"
	}
	return "echo"
}

func echoArgs(msg string) []any {
	if runtime.GOOS == "windows" {
		return []any{"/C", "echo " + msg}
	}
	return []any{msg}
}

func failCommand() (string, []any) {
	if runtime.GOOS == "windows" {
		return "cmd", []any{"/C", "exit /b 42"}
	}
	return "sh", []any{"-c", "exit 42"}
}

func makeTask(payload map[string]any) execute.AssignedTask {
	return execute.AssignedTask{
		InstanceID:     1,
		TenantID:       "default",
		HandlerType:    "exec",
		HandlerPayload: payload,
		TimeoutSec:     10,
	}
}

func TestExecHandler_Success(t *testing.T) {
	h := &ExecHandler{}
	result := h.Execute(context.Background(), makeTask(map[string]any{
		"command": echoCommand(),
		"args":    echoArgs("hello"),
	}))
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.ErrorMsg)
	}
	if result.ResultCode != "0" {
		t.Fatalf("expected result_code=0, got %q", result.ResultCode)
	}
}

func TestExecHandler_ExitError(t *testing.T) {
	h := &ExecHandler{}
	cmd, args := failCommand()
	argsAny := make([]any, len(args))
	copy(argsAny, args)
	result := h.Execute(context.Background(), makeTask(map[string]any{
		"command": cmd,
		"args":    argsAny,
	}))
	if result.Success {
		t.Fatal("expected failure")
	}
	if result.ResultCode != "42" {
		t.Fatalf("expected result_code=42, got %q", result.ResultCode)
	}
}

func TestExecHandler_Timeout(t *testing.T) {
	h := &ExecHandler{}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	var cmd string
	var args []any
	if runtime.GOOS == "windows" {
		cmd = "cmd"
		args = []any{"/C", "ping -n 10 127.0.0.1 >nul"}
	} else {
		cmd = "sleep"
		args = []any{"10"}
	}

	result := h.Execute(ctx, makeTask(map[string]any{
		"command": cmd,
		"args":    args,
	}))
	if result.Success {
		t.Fatal("expected failure on timeout")
	}
	if result.ResultCode != "timeout" {
		t.Fatalf("expected result_code=timeout, got %q", result.ResultCode)
	}
}

func TestExecHandler_InvalidPayload_MissingCommand(t *testing.T) {
	h := &ExecHandler{}
	result := h.Execute(context.Background(), makeTask(map[string]any{}))
	if result.Success {
		t.Fatal("expected failure")
	}
	if result.ResultCode != "invalid_payload" {
		t.Fatalf("expected result_code=invalid_payload, got %q", result.ResultCode)
	}
}

func TestExecHandler_InvalidPayload_BadArgs(t *testing.T) {
	h := &ExecHandler{}
	result := h.Execute(context.Background(), makeTask(map[string]any{
		"command": "echo",
		"args":    "not-an-array",
	}))
	if result.Success {
		t.Fatal("expected failure")
	}
	if result.ResultCode != "invalid_payload" {
		t.Fatalf("expected result_code=invalid_payload, got %q", result.ResultCode)
	}
}

func TestExecHandler_CommandNotFound(t *testing.T) {
	h := &ExecHandler{}
	result := h.Execute(context.Background(), makeTask(map[string]any{
		"command": "/nonexistent/binary/xyz",
	}))
	if result.Success {
		t.Fatal("expected failure")
	}
	if result.ResultCode != "error" {
		t.Fatalf("expected result_code=error, got %q", result.ResultCode)
	}
}
