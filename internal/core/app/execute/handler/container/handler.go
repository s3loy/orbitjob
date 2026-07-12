package container

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"orbitjob/internal/core/app/execute"
)

const (
	defaultPollInterval = time.Second
	defaultTTL          = 10 * time.Minute
)

var dnsUnsafe = regexp.MustCompile(`[^a-z0-9-]+`)

type Config struct {
	Client           kubernetes.Interface
	Namespace        string
	PollInterval     time.Duration
	TTLAfterFinished time.Duration
	RequireDigest    bool
}

type Handler struct {
	cfg Config
}

func New(cfg Config) *Handler {
	if cfg.Client == nil {
		panic("container handler requires a Kubernetes client")
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = defaultPollInterval
	}
	if cfg.TTLAfterFinished <= 0 {
		cfg.TTLAfterFinished = defaultTTL
	}
	return &Handler{cfg: cfg}
}

func (h *Handler) Execute(ctx context.Context, task execute.AssignedTask) execute.Result {
	payload, err := parsePayload(task.HandlerPayload, h.cfg.RequireDigest)
	if err != nil {
		return failure("invalid_payload", err)
	}
	if h.cfg.Namespace == "" {
		return failure("invalid_config", fmt.Errorf("container namespace is required"))
	}

	job := buildJob(h.cfg, task, payload)
	jobs := h.cfg.Client.BatchV1().Jobs(h.cfg.Namespace)
	created, err := jobs.Create(ctx, job, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		created, err = jobs.Get(ctx, job.Name, metav1.GetOptions{})
		if err == nil && created.Annotations["orbitjob.io/spec-hash"] != job.Annotations["orbitjob.io/spec-hash"] {
			return failure("ownership_mismatch", fmt.Errorf("existing Kubernetes Job spec does not match run %s", task.RunID))
		}
	}
	if err != nil {
		return failure("create_failed", err)
	}

	ticker := time.NewTicker(h.cfg.PollInterval)
	defer ticker.Stop()
	for {
		result, done := resultFromJob(created)
		if done {
			return result
		}
		select {
		case <-ctx.Done():
			policy := metav1.DeletePropagationForeground
			_ = jobs.Delete(context.Background(), created.Name, metav1.DeleteOptions{PropagationPolicy: &policy})
			return failure("timeout", ctx.Err())
		case <-ticker.C:
			created, err = jobs.Get(ctx, created.Name, metav1.GetOptions{})
			if err != nil {
				return failure("observe_failed", err)
			}
		}
	}
}

type payload struct {
	Image              string
	Command            []string
	Args               []string
	Env                []corev1.EnvVar
	Resources          corev1.ResourceRequirements
	ImagePullPolicy    corev1.PullPolicy
	ServiceAccountName string
}

func parsePayload(raw map[string]any, requireDigest bool) (payload, error) {
	image, ok := raw["image"].(string)
	if !ok || strings.TrimSpace(image) == "" {
		return payload{}, fmt.Errorf("image must be a non-empty string")
	}
	if requireDigest && !strings.Contains(image, "@sha256:") {
		return payload{}, fmt.Errorf("image must use an immutable sha256 digest")
	}

	command, err := stringSlice(raw["command"], "command")
	if err != nil {
		return payload{}, err
	}
	args, err := stringSlice(raw["args"], "args")
	if err != nil {
		return payload{}, err
	}

	pullPolicy := corev1.PullIfNotPresent
	if value, ok := raw["image_pull_policy"]; ok {
		policy, ok := value.(string)
		if !ok || (policy != string(corev1.PullAlways) && policy != string(corev1.PullIfNotPresent) && policy != string(corev1.PullNever)) {
			return payload{}, fmt.Errorf("image_pull_policy must be Always, IfNotPresent, or Never")
		}
		pullPolicy = corev1.PullPolicy(policy)
	}

	serviceAccount := "orbitjob-task"
	if value, ok := raw["service_account_name"]; ok {
		serviceAccount, ok = value.(string)
		if !ok || serviceAccount == "" {
			return payload{}, fmt.Errorf("service_account_name must be a non-empty string")
		}
	}

	return payload{
		Image: image, Command: command, Args: args,
		ImagePullPolicy: pullPolicy, ServiceAccountName: serviceAccount,
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("64Mi")},
			Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("512Mi")},
		},
	}, nil
}

func stringSlice(value any, field string) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an array of strings", field)
	}
	out := make([]string, 0, len(items))
	for i, item := range items {
		text, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("%s[%d] must be a string", field, i)
		}
		out = append(out, text)
	}
	return out, nil
}

func buildJob(cfg Config, task execute.AssignedTask, p payload) *batchv1.Job {
	zero := int32(0)
	ttl := int32(cfg.TTLAfterFinished.Seconds())
	deadline := int64(task.TimeoutSec)
	runID := sanitizeName(task.RunID)
	name := "orbitjob-" + runID
	if len(name) > 63 {
		name = name[:63]
	}
	hash := specHash(task.HandlerPayload)
	labels := map[string]string{
		"app.kubernetes.io/managed-by": "orbitjob",
		"orbitjob.io/tenant":           sanitizeLabel(task.TenantID),
		"orbitjob.io/job-id":           fmt.Sprint(task.JobID),
		"orbitjob.io/instance-id":      fmt.Sprint(task.InstanceID),
	}
	nonRoot, readOnly, noEscalation := true, true, false
	uid := int64(65534)
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: cfg.Namespace, Labels: labels, Annotations: map[string]string{"orbitjob.io/run-id": task.RunID, "orbitjob.io/spec-hash": hash}},
		Spec: batchv1.JobSpec{
			BackoffLimit: &zero, TTLSecondsAfterFinished: &ttl, ActiveDeadlineSeconds: &deadline,
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: labels}, Spec: corev1.PodSpec{
				RestartPolicy: corev1.RestartPolicyNever, AutomountServiceAccountToken: ptr(false), ServiceAccountName: p.ServiceAccountName,
				SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: &nonRoot, RunAsUser: &uid, SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
				Containers: []corev1.Container{{Name: "task", Image: p.Image, Command: p.Command, Args: p.Args, Env: p.Env, ImagePullPolicy: p.ImagePullPolicy, Resources: p.Resources,
					SecurityContext: &corev1.SecurityContext{RunAsNonRoot: &nonRoot, RunAsUser: &uid, ReadOnlyRootFilesystem: &readOnly, AllowPrivilegeEscalation: &noEscalation, Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}},
				}},
			}},
		},
	}
}

func resultFromJob(job *batchv1.Job) (execute.Result, bool) {
	for _, condition := range job.Status.Conditions {
		if condition.Status != corev1.ConditionTrue {
			continue
		}
		switch condition.Type {
		case batchv1.JobComplete:
			return execute.Result{Success: true, ResultCode: "completed"}, true
		case batchv1.JobFailed:
			return failure("pod_failed", fmt.Errorf("%s: %s", condition.Reason, condition.Message)), true
		}
	}
	return execute.Result{}, false
}

func failure(code string, err error) execute.Result {
	return execute.Result{Success: false, ResultCode: code, ErrorMsg: err.Error()}
}

func sanitizeName(value string) string {
	value = strings.ToLower(value)
	value = strings.Trim(dnsUnsafe.ReplaceAllString(value, "-"), "-")
	if value == "" {
		return "run"
	}
	return value
}

func sanitizeLabel(value string) string {
	value = sanitizeName(value)
	if len(value) > 63 {
		return value[:63]
	}
	return value
}

func specHash(value map[string]any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func ptr[T any](value T) *T { return &value }

var _ execute.Handler = (*Handler)(nil)
