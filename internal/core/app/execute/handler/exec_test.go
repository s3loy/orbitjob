package handler

import (
	"bytes"
	"context"
	"runtime"
	"slices"
	"strings"
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
	return "false", []any{}
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

func TestExec_Success(t *testing.T) {
	h := &Exec{}
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

func TestExec_ExitError(t *testing.T) {
	h := &Exec{}
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
	if result.ResultCode == "0" || result.ResultCode == "" {
		t.Fatalf("expected non-zero result_code, got %q", result.ResultCode)
	}
}

func TestExec_Timeout(t *testing.T) {
	h := &Exec{}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	var cmd string
	var args []any
	if runtime.GOOS == "windows" {
		cmd = "ping"
		args = []any{"-n", "10", "127.0.0.1"}
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

func TestExec_InvalidPayload_MissingCommand(t *testing.T) {
	h := &Exec{}
	result := h.Execute(context.Background(), makeTask(map[string]any{}))
	if result.Success {
		t.Fatal("expected failure")
	}
	if result.ResultCode != "invalid_payload" {
		t.Fatalf("expected result_code=invalid_payload, got %q", result.ResultCode)
	}
}

func TestExec_InvalidPayload_BadArgs(t *testing.T) {
	h := &Exec{}
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

// ---------------------------------------------------------------------------
// isBlacklisted tests
// ---------------------------------------------------------------------------

func TestIsBlacklisted(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want bool
	}{
		// Explicity blacklisted
		{"LD_PRELOAD", "LD_PRELOAD", true},
		{"LD_LIBRARY_PATH", "LD_LIBRARY_PATH", true},
		{"LD_AUDIT", "LD_AUDIT", true},
		{"PYTHONPATH", "PYTHONPATH", true},
		{"PERL5LIB", "PERL5LIB", true},
		{"RUBYLIB", "RUBYLIB", true},
		// DYLD_ prefix — blocked by prefix check
		{"DYLD_LIBRARY_PATH", "DYLD_LIBRARY_PATH", true},
		{"DYLD_INSERT_LIBRARIES", "DYLD_INSERT_LIBRARIES", true},
		{"DYLD_FRAMEWORK_PATH", "DYLD_FRAMEWORK_PATH", true},
		// Non-blacklisted
		{"PATH", "PATH", false},
		{"HOME", "HOME", false},
		{"USER", "USER", false},
		{"MY_CUSTOM_VAR", "MY_CUSTOM_VAR", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isBlacklisted(tt.key)
			if got != tt.want {
				t.Errorf("isBlacklisted(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// truncate tests
// ---------------------------------------------------------------------------

func TestTruncate(t *testing.T) {
	tests := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{"shorter than n", "abc", 10, "abc"},
		{"equal to n", "abcde", 5, "abcde"},
		{"longer than n", "abcdefghi", 5, "abcde"},
		{"empty string", "", 5, ""},
		{"zero n", "abc", 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.s, tt.n)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.s, tt.n, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// limitedWriter.Write tests
// ---------------------------------------------------------------------------

func TestLimitedWriter_Write(t *testing.T) {
	tests := []struct {
		name      string
		limit     int
		writes    []string
		wantBuf   string
		wantFirst int // n returned from first write
	}{
		{
			name:      "within limit",
			limit:     10,
			writes:    []string{"hello"},
			wantBuf:   "hello",
			wantFirst: 5,
		},
		{
			name:      "exact limit",
			limit:     5,
			writes:    []string{"hello"},
			wantBuf:   "hello",
			wantFirst: 5,
		},
		{
			name:      "exceeds limit single write",
			limit:     3,
			writes:    []string{"hello"},
			wantBuf:   "hel",
			wantFirst: 3,
		},
		{
			name:      "multiple writes within limit",
			limit:     10,
			writes:    []string{"hello", "world"},
			wantBuf:   "helloworld",
			wantFirst: 5,
		},
		{
			name:      "multiple writes exceeds limit",
			limit:     7,
			writes:    []string{"hello", "world"},
			wantBuf:   "hellowo",
			wantFirst: 5,
		},
		{
			name:      "empty write",
			limit:     10,
			writes:    []string{""},
			wantBuf:   "",
			wantFirst: 0,
		},
		{
			name:      "zero limit",
			limit:     0,
			writes:    []string{"hello"},
			wantBuf:   "",
			wantFirst: 5, // reports len(p) but writes nothing
		},
		{
			name:      "write after exhausted",
			limit:     3,
			writes:    []string{"abc", "def"},
			wantBuf:   "abc",
			wantFirst: 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := new(bytes.Buffer)
			w := &limitedWriter{buf: buf, limit: tt.limit}
			n, err := w.Write([]byte(tt.writes[0]))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if n != tt.wantFirst {
				t.Errorf("first Write n = %d, want %d", n, tt.wantFirst)
			}
			for i := 1; i < len(tt.writes); i++ {
				_, _ = w.Write([]byte(tt.writes[i]))
			}
			got := buf.String()
			if got != tt.wantBuf {
				t.Errorf("buf.String() = %q, want %q", got, tt.wantBuf)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// buildEnv tests
// ---------------------------------------------------------------------------

func TestBuildEnv_BaseEnv(t *testing.T) {
	env := buildEnv(nil)
	foundHome := false
	foundUser := false
	for _, e := range env {
		if e == "HOME=/tmp" {
			foundHome = true
		}
		if e == "USER=nobody" {
			foundUser = true
		}
	}
	if !foundHome {
		t.Error("expected HOME=/tmp in env")
	}
	if !foundUser {
		t.Error("expected USER=nobody in env")
	}
}

func TestBuildEnv_WhitelistInherited(t *testing.T) {
	env := buildEnv(nil)
	found := false
	for _, e := range env {
		if len(e) > 5 && e[:5] == "PATH=" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected PATH inherited from parent env")
	}
}

func TestBuildEnv_UserEnvAdded(t *testing.T) {
	env := buildEnv(map[string]string{"MY_VAR": "hello"})
	if !slices.Contains(env, "MY_VAR=hello") {
		t.Error("expected MY_VAR=hello in env")
	}
}

func TestBuildEnv_BlacklistedStripped(t *testing.T) {
	env := buildEnv(map[string]string{
		"LD_PRELOAD": "/evil.so",
		"OK_VAR":     "ok",
	})
	for _, e := range env {
		if strings.HasPrefix(e, "LD_PRELOAD=") {
			t.Errorf("LD_PRELOAD should be stripped, got %q", e)
		}
	}
	if !slices.Contains(env, "OK_VAR=ok") {
		t.Error("expected OK_VAR=ok in env")
	}
}

func TestBuildEnv_DYLDPrefixStripped(t *testing.T) {
	env := buildEnv(map[string]string{
		"DYLD_LIBRARY_PATH": "/bad",
		"SAFE_VAR":          "safe",
	})
	for _, e := range env {
		if strings.HasPrefix(e, "DYLD_") {
			t.Errorf("DYLD_ prefixed var should be stripped: %q", e)
		}
	}
	if !slices.Contains(env, "SAFE_VAR=safe") {
		t.Error("expected SAFE_VAR=safe in env")
	}
}

func TestBuildEnv_EmptyUserEnv(t *testing.T) {
	env := buildEnv(map[string]string{})
	// Should still have base env + whitelist
	if len(env) < 2 {
		t.Errorf("expected at least base env vars, got %d entries: %v", len(env), env)
	}
}

// ---------------------------------------------------------------------------
// parseExecPayload tests
// ---------------------------------------------------------------------------

func TestParseExecPayload(t *testing.T) {
	tests := []struct {
		name        string
		payload     map[string]any
		wantCmd     string
		wantArgs    []string
		wantEnvKeys []string
		wantErr     bool
		errContains string
	}{
		{
			name: "valid with all fields",
			payload: map[string]any{
				"command": "echo",
				"args":    []any{"hello", "world"},
				"env":     map[string]any{"KEY": "value"},
			},
			wantCmd:     "echo",
			wantArgs:    []string{"hello", "world"},
			wantEnvKeys: []string{"KEY"},
		},
		{
			name: "command only",
			payload: map[string]any{
				"command": "echo",
			},
			wantCmd: "echo",
		},
		{
			name:        "missing command",
			payload:     map[string]any{},
			wantErr:     true,
			errContains: "missing required field: command",
		},
		{
			name:        "command not string",
			payload:     map[string]any{"command": 42},
			wantErr:     true,
			errContains: "command must be a non-empty string",
		},
		{
			name:        "command empty",
			payload:     map[string]any{"command": ""},
			wantErr:     true,
			errContains: "command must be a non-empty string",
		},
		{
			name: "args not array",
			payload: map[string]any{
				"command": "echo",
				"args":    "not-an-array",
			},
			wantErr:     true,
			errContains: "args must be an array of strings",
		},
		{
			name: "args element not string",
			payload: map[string]any{
				"command": "echo",
				"args":    []any{"ok", 42},
			},
			wantErr:     true,
			errContains: "args[1] must be a string",
		},
		{
			name: "env not map",
			payload: map[string]any{
				"command": "echo",
				"env":     "not-a-map",
			},
			wantErr:     true,
			errContains: "env must be a map of string to string",
		},
		{
			name: "env value not string",
			payload: map[string]any{
				"command": "echo",
				"env":     map[string]any{"KEY": 42},
			},
			wantErr:     true,
			errContains: "must be a string",
		},
		{
			name:        "command with path separator",
			payload:     map[string]any{"command": "/bin/rm"},
			wantErr:     true,
			errContains: "path separators",
		},
		{
			name:        "blocked shell command",
			payload:     map[string]any{"command": "bash"},
			wantErr:     true,
			errContains: "not allowed",
		},
		{
			name:        "arg with shell metacharacter",
			payload:     map[string]any{"command": "echo", "args": []any{"hello; rm -rf /"}},
			wantErr:     true,
			errContains: "disallowed characters",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, args, env, err := parseExecPayload(tt.payload)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cmd != tt.wantCmd {
				t.Errorf("command = %q, want %q", cmd, tt.wantCmd)
			}
			if len(args) != len(tt.wantArgs) {
				t.Fatalf("args len = %d, want %d", len(args), len(tt.wantArgs))
			}
			for i, a := range args {
				if a != tt.wantArgs[i] {
					t.Errorf("args[%d] = %q, want %q", i, a, tt.wantArgs[i])
				}
			}
			if tt.wantEnvKeys != nil {
				if env == nil {
					t.Fatal("expected non-nil env")
				}
				for _, k := range tt.wantEnvKeys {
					if _, ok := env[k]; !ok {
						t.Errorf("expected env key %q", k)
					}
				}
			}
		})
	}
}

func TestExec_CommandNotFound(t *testing.T) {
	h := &Exec{}
	result := h.Execute(context.Background(), makeTask(map[string]any{
		"command": "nonexistent_binary_xyz",
	}))
	if result.Success {
		t.Fatal("expected failure")
	}
	if result.ResultCode != "error" {
		t.Fatalf("expected result_code=error, got %q", result.ResultCode)
	}
}
