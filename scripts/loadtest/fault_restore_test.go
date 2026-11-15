package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCanceledDatabaseFaultAlwaysRestoresService(t *testing.T) {
	bin := t.TempDir()
	down, restored := filepath.Join(bin, "down"), filepath.Join(bin, "restored")
	script := "#!/bin/sh\ncase \"$*\" in\n*--replicas=0*) printf down > \"$FAULT_DOWN\";;\n*--replicas=1*) printf restored > \"$FAULT_RESTORED\";;\nesac\n"
	if err := os.WriteFile(filepath.Join(bin, "kubectl"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("FAULT_DOWN", down)
	t.Setenv("FAULT_RESTORED", restored)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan struct{})
	recorder := &FaultRecorder{}
	go func() {
		defer close(done)
		injectFaults(ctx, time.Now(), []FaultSpec{{Name: "postgres", DisconnectFor: time.Hour}}, recorder)
	}()
	for {
		if _, err := os.Stat(down); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("injection never reached the disconnect step")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("fault goroutine did not finish")
	}
	if _, err := os.Stat(restored); err != nil {
		t.Fatalf("cancellation left PostgreSQL scaled down: %v", err)
	}
	if records := recorder.Records(); len(records) != 1 || records[0].InjectError == "" {
		t.Fatalf("canceled fault must retain failure evidence: %+v", records)
	}
}
