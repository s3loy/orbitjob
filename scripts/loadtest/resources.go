package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ResourceSnapshot captures the capacity boundaries that constrain loadtest
// throughput. All CPU values are in fractional cores and memory in GiB.
type ResourceSnapshot struct {
	DockerCPU               int
	DockerMemoryGi          int
	TaskNamespaceCPULimit   float64
	TaskNamespaceMemGiLimit float64
	TaskCPULimit            float64
	TaskMemLimitGi          float64
	WorkerCPULimit          float64
	WorkerMemLimitGi        float64
	AdminAPICPULimit        float64
	AdminAPIMemLimitGi      float64
}

// TuningParameters holds the computed loadtest settings derived from
// ResourceSnapshot and the load config.
type TuningParameters struct {
	PhaseRates                     map[string]int
	MaxActive                      map[string]int
	EffectiveWorkerCapacityMax     int
	RecommendedWorkerCapacity      int
	RecommendedTriggerRPSPerTenant int
	EstimatedManualInstances       int
	EstimatedCronInstances         int
}

// InspectResources reads Docker host capacity and Kubernetes resource limits.
func InspectResources(ctx context.Context, rm ResourceModelConfig) (ResourceSnapshot, error) {
	var snap ResourceSnapshot

	cpu, mem, err := dockerCapacity(ctx)
	if err != nil {
		return snap, fmt.Errorf("docker capacity: %w", err)
	}
	snap.DockerCPU = cpu
	snap.DockerMemoryGi = mem

	quotaCPU, quotaMem, err := taskNamespaceQuota(ctx)
	if err != nil {
		return snap, fmt.Errorf("task namespace quota: %w", err)
	}
	snap.TaskNamespaceCPULimit = quotaCPU
	snap.TaskNamespaceMemGiLimit = quotaMem

	taskCPU, taskMem, err := taskContainerLimits(ctx)
	if err != nil {
		return snap, fmt.Errorf("task container limits: %w", err)
	}
	snap.TaskCPULimit = taskCPU
	snap.TaskMemLimitGi = taskMem

	workerCPU, workerMem, err := deploymentLimits(ctx, "orbitjob-worker")
	if err != nil {
		return snap, fmt.Errorf("worker deployment limits: %w", err)
	}
	snap.WorkerCPULimit = workerCPU
	snap.WorkerMemLimitGi = workerMem

	adminCPU, adminMem, err := deploymentLimits(ctx, "orbitjob-admin-api")
	if err != nil {
		return snap, fmt.Errorf("admin-api deployment limits: %w", err)
	}
	snap.AdminAPICPULimit = adminCPU
	snap.AdminAPIMemLimitGi = adminMem

	return snap, nil
}

// ComputeTuning derives phase rates, concurrency limits, and platform settings
// from the observed resource snapshot.
func ComputeTuning(snap ResourceSnapshot, cfg Config, rm ResourceModelConfig) (TuningParameters, error) {
	params := TuningParameters{
		PhaseRates: make(map[string]int),
		MaxActive:  make(map[string]int),
	}

	availableTaskCPU := minFloat(snap.TaskNamespaceCPULimit, float64(snap.DockerCPU)-rm.SystemReserveCPU)
	availableTaskMem := minFloat(snap.TaskNamespaceMemGiLimit, float64(snap.DockerMemoryGi)-rm.SystemReserveMemGi)

	cpuBound := availableTaskCPU / snap.TaskCPULimit
	memBound := availableTaskMem / snap.TaskMemLimitGi

	rawCapacity := minFloat(cpuBound, memBound)
	if rawCapacity < 1 {
		return params, fmt.Errorf("insufficient task namespace resources: raw capacity %.2f < 1", rawCapacity)
	}

	params.EffectiveWorkerCapacityMax = int(rawCapacity * rm.HeadroomFactor)
	if params.EffectiveWorkerCapacityMax < 1 {
		params.EffectiveWorkerCapacityMax = 1
	}
	params.RecommendedWorkerCapacity = params.EffectiveWorkerCapacityMax

	cronCount := cfg.Definitions.ProductTriggerTypes["cron"]
	cronInterval := time.Duration(cfg.Definitions.CronIntervalMinutes) * time.Minute
	if cronInterval > 0 {
		params.EstimatedCronInstances = cronCount * int(cfg.Duration/cronInterval)
	}

	requiredManual := cfg.MinimumInstances - params.EstimatedCronInstances
	if requiredManual < 0 {
		requiredManual = 0
	}
	params.EstimatedManualInstances = requiredManual

	sustainableTriggersPerSec := float64(params.EffectiveWorkerCapacityMax) / float64(rm.TaskAvgDurationSec)
	sustainableTriggersPerMinute := sustainableTriggersPerSec * 60

	phaseWeights := map[string]float64{
		"warmup":        0.10,
		"correctness":   0.20,
		"steady":        0.30,
		"ramp":          0.50,
		"peak":          1.00,
		"recovery":      0.30,
		"fault-recovery": 0.60,
	}
	phaseBuffers := map[string]float64{
		"warmup":        1.0,
		"correctness":   1.0,
		"steady":        1.0,
		"ramp":          1.2,
		"peak":          1.5,
		"recovery":      1.0,
		"fault-recovery": 1.2,
	}

	weightedMinutes := 0.0
	for _, phase := range cfg.Phases {
		w, ok := phaseWeights[phase.Name]
		if !ok {
			w = 0.5
		}
		weightedMinutes += phase.Duration.Minutes() * w
	}
	if weightedMinutes <= 0 {
		return params, fmt.Errorf("no weighted phase minutes to distribute")
	}

	// Peak rate is the rate at weight==1.0. It must be high enough to generate
	// the required manual instances when integrated over the weighted phase shape.
	peakRatePerMinute := float64(requiredManual) / weightedMinutes
	// Cap peak at the sustainable rate times the allowed overshoot.
	maxPeakRate := sustainableTriggersPerMinute * rm.PeakOvershoot
	if peakRatePerMinute > maxPeakRate {
		peakRatePerMinute = maxPeakRate
	}
	if peakRatePerMinute < 1 {
		peakRatePerMinute = 1
	}

	// Initial rounded rates, then distribute any deficit so the total manual
	// instances reach the required count.
	type phaseRate struct {
		name string
		rate int
		loss float64
	}
	phaseRates := make([]phaseRate, 0, len(cfg.Phases))
	totalManual := 0
	for _, phase := range cfg.Phases {
		w := phaseWeights[phase.Name]
		if _, ok := phaseWeights[phase.Name]; !ok {
			w = 0.5
		}
		raw := peakRatePerMinute * w
		rate := int(math.Round(raw))
		if rate < 1 {
			rate = 1
		}
		phaseRates = append(phaseRates, phaseRate{name: phase.Name, rate: rate, loss: raw - float64(rate)})
		params.MaxActive[phase.Name] = int(float64(params.EffectiveWorkerCapacityMax) * phaseBuffers[phase.Name])
		if params.MaxActive[phase.Name] < 1 {
			params.MaxActive[phase.Name] = 1
		}
		totalManual += int(phase.Duration.Minutes()) * rate
	}
	if totalManual < requiredManual {
		deficit := requiredManual - totalManual
		// Sort by descending loss so we bump phases that were rounded down most.
		sort.Slice(phaseRates, func(i, j int) bool { return phaseRates[i].loss < phaseRates[j].loss })
		for deficit > 0 {
			for i := range phaseRates {
				if deficit <= 0 {
					break
				}
				phaseRates[i].rate++
				deficit--
			}
		}
	}
	for _, pr := range phaseRates {
		params.PhaseRates[pr.name] = pr.rate
	}

	// Recompute estimated manual instances from final rates.
	totalManual = 0
	for _, phase := range cfg.Phases {
		totalManual += int(phase.Duration.Minutes()) * params.PhaseRates[phase.Name]
	}
	params.EstimatedManualInstances = totalManual

	params.RecommendedTriggerRPSPerTenant = int(peakRatePerMinute/60.0/float64(len(cfg.Tenants))) + 1
	if params.RecommendedTriggerRPSPerTenant < 10 {
		params.RecommendedTriggerRPSPerTenant = 10
	}

	return params, nil
}

func dockerCapacity(ctx context.Context) (cpu int, memGi int, err error) {
	out, err := exec.CommandContext(ctx, "docker", "info", "--format", "{{.NCPU}} {{.MemTotal}}").Output()
	if err != nil {
		return 0, 0, err
	}
	parts := strings.Fields(string(out))
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("unexpected docker info output: %s", string(out))
	}
	cpu, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("parse docker cpu: %w", err)
	}
	memBytes, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parse docker memory: %w", err)
	}
	return cpu, int(memBytes / (1 << 30)), nil
}

func taskNamespaceQuota(ctx context.Context) (cpu float64, memGi float64, err error) {
	out, err := exec.CommandContext(ctx, "kubectl", "get", "resourcequota", "-n", "orbitjob-tasks", "-o", "json").Output()
	if err != nil {
		return 0, 0, err
	}
	var list struct {
		Items []struct {
			Spec struct {
				Hard map[string]string `json:"hard"`
			} `json:"spec"`
		} `json:"items"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return 0, 0, err
	}
	for _, item := range list.Items {
		if c, ok := item.Spec.Hard["limits.cpu"]; ok {
			cpu, err = parseCPU(c)
			if err != nil {
				return 0, 0, err
			}
		}
		if m, ok := item.Spec.Hard["limits.memory"]; ok {
			memGi, err = parseMemoryGi(m)
			if err != nil {
				return 0, 0, err
			}
		}
	}
	return cpu, memGi, nil
}

func taskContainerLimits(ctx context.Context) (cpu float64, memGi float64, err error) {
	out, err := exec.CommandContext(ctx, "kubectl", "get", "limitrange", "-n", "orbitjob-tasks", "-o", "json").Output()
	if err != nil {
		return 0, 0, err
	}
	var list struct {
		Items []struct {
			Spec struct {
				Limits []struct {
					Type    string            `json:"type"`
					Default map[string]string `json:"default"`
					Max     map[string]string `json:"max"`
				} `json:"limits"`
			} `json:"spec"`
		} `json:"items"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return 0, 0, err
	}
	for _, item := range list.Items {
		for _, limit := range item.Spec.Limits {
			if limit.Type != "Container" {
				continue
			}
			c, ok := limit.Max["cpu"]
			if !ok {
				c = limit.Default["cpu"]
			}
			m, ok := limit.Max["memory"]
			if !ok {
				m = limit.Default["memory"]
			}
			if c != "" {
				cpu, err = parseCPU(c)
				if err != nil {
					return 0, 0, err
				}
			}
			if m != "" {
				memGi, err = parseMemoryGi(m)
				if err != nil {
					return 0, 0, err
				}
			}
		}
	}
	return cpu, memGi, nil
}

func deploymentLimits(ctx context.Context, name string) (cpu float64, memGi float64, err error) {
	out, err := exec.CommandContext(ctx, "kubectl", "get", "deployment", name, "-n", "orbitjob", "-o", "json").Output()
	if err != nil {
		return 0, 0, err
	}
	var dep struct {
		Spec struct {
			Template struct {
				Spec struct {
					Containers []struct {
						Resources struct {
							Limits map[string]string `json:"limits"`
						} `json:"resources"`
					} `json:"containers"`
				} `json:"spec"`
			} `json:"template"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(out, &dep); err != nil {
		return 0, 0, err
	}
	if len(dep.Spec.Template.Spec.Containers) == 0 {
		return 0, 0, fmt.Errorf("deployment %s has no containers", name)
	}
	limits := dep.Spec.Template.Spec.Containers[0].Resources.Limits
	if c, ok := limits["cpu"]; ok {
		cpu, err = parseCPU(c)
		if err != nil {
			return 0, 0, err
		}
	}
	if m, ok := limits["memory"]; ok {
		memGi, err = parseMemoryGi(m)
		if err != nil {
			return 0, 0, err
		}
	}
	return cpu, memGi, nil
}

func parseCPU(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "m") {
		n, err := strconv.ParseFloat(strings.TrimSuffix(s, "m"), 64)
		if err != nil {
			return 0, err
		}
		return n / 1000.0, nil
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	return n, nil
}

func parseMemoryGi(s string) (float64, error) {
	s = strings.TrimSpace(s)
	multipliers := map[string]float64{
		"Ki": 1 << 10,
		"Mi": 1 << 20,
		"Gi": 1 << 30,
		"Ti": 1 << 40,
	}
	for suffix, mult := range multipliers {
		if strings.HasSuffix(s, suffix) {
			n, err := strconv.ParseFloat(strings.TrimSuffix(s, suffix), 64)
			if err != nil {
				return 0, err
			}
			return (n * mult) / (1 << 30), nil
		}
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	// Bare number is bytes.
	return n / (1 << 30), nil
}

func minFloat(vals ...float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	m := vals[0]
	for _, v := range vals[1:] {
		if v < m {
			m = v
		}
	}
	return m
}
