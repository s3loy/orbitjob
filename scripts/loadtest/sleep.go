package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"time"
)

// caffeinateWaitFlag tells caffeinate to hold the assertion until the named
// process exits, so the assertion cannot outlive the run that asked for it.
const caffeinateWaitFlag = "-w"

// holdSleepAssertion keeps the host awake for the duration of a run.
//
// The engine paces the schedule against the monotonic clock, and on darwin that
// clock stops while the machine is asleep. A host that sleeps between
// maintenance windows therefore does not finish a 30-minute profile in 30
// minutes: the schedule needs 30 minutes of *awake* time and stretches across
// however many hours that takes. The run is not hung and nothing is slow -- but
// every latency number it reports is meaningless, and the final event may not
// be reached at all. Measured on a host sleeping 900s out of every 960s, the
// first 11 minutes of a smoke schedule took 1h46m of wall clock.
//
// Nothing downstream can repair that after the fact, so the assertion is taken
// up front. Platforms without a helper get a no-op release rather than a
// failure: a Linux CI runner does not sleep.
func holdSleepAssertion() func() {
	if runtime.GOOS != "darwin" {
		return func() {}
	}
	path, err := exec.LookPath("caffeinate")
	if err != nil {
		fmt.Println("loadtest: caffeinate not found; if the host sleeps the schedule will stretch")
		return func() {}
	}
	// -i holds off idle sleep, -s holds off system sleep while on AC power, and
	// -w ties both to this process. The process is `go run`'s child, so the
	// assertion ends exactly when the run does.
	cmd := exec.Command(path, "-i", "-s", caffeinateWaitFlag, strconv.Itoa(os.Getpid()))
	if err := cmd.Start(); err != nil {
		fmt.Printf("loadtest: could not hold a sleep assertion: %v\n", err)
		return func() {}
	}
	return func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}
}

// reportSuspension compares wall-clock time against the monotonic clock the
// engine paced against. The two agree unless the host suspended, which is the
// one thing that silently invalidates a run's numbers.
func reportSuspension(wallStart time.Time, monotonicElapsed time.Duration) {
	wallElapsed := time.Duration(time.Now().Unix()-wallStart.Unix()) * time.Second
	suspended := wallElapsed - monotonicElapsed
	if suspended < time.Minute {
		return
	}
	fmt.Printf("loadtest: the host was suspended for about %s; the schedule stretched to %s of wall clock and the latency numbers are not usable\n",
		suspended.Round(time.Second), wallElapsed.Round(time.Second))
}
