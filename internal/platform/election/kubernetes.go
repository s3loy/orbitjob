package election

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// KubernetesConfig holds connection parameters for Lease-based coordination.
type KubernetesConfig struct {
	// Namespace holds the Lease objects. It must be the namespace the process
	// runs in, because the Lease is namespaced and cross-namespace writes would
	// need a broader role than leader election warrants.
	Namespace string
	// Identity distinguishes competing instances. It defaults to the pod name.
	Identity string
	// LeaseDuration is how long a holder may stay leader without renewing.
	LeaseDuration time.Duration
	// RenewDeadline is how long a holder keeps trying to renew before giving up.
	RenewDeadline time.Duration
	// RetryPeriod is the interval between acquire and renew attempts.
	RetryPeriod time.Duration
	// Client performs the Lease operations. Defaults to an in-cluster client.
	Client kubernetes.Interface
}

func (c KubernetesConfig) withDefaults() (KubernetesConfig, error) {
	if c.Namespace == "" {
		return c, errors.New("kubernetes election namespace is required")
	}
	if c.Identity == "" {
		c.Identity = os.Getenv("POD_NAME")
	}
	if c.Identity == "" {
		host, err := os.Hostname()
		if err != nil {
			return c, fmt.Errorf("derive election identity: %w", err)
		}
		c.Identity = host
	}
	if c.LeaseDuration <= 0 {
		c.LeaseDuration = 15 * time.Second
	}
	if c.RenewDeadline <= 0 {
		c.RenewDeadline = 10 * time.Second
	}
	if c.RetryPeriod <= 0 {
		c.RetryPeriod = 2 * time.Second
	}
	// A renew deadline at or above the lease duration means the holder can
	// believe it is leader after another instance has already taken over.
	if c.RenewDeadline >= c.LeaseDuration {
		return c, fmt.Errorf("renew deadline %s must be shorter than lease duration %s",
			c.RenewDeadline, c.LeaseDuration)
	}
	return c, nil
}

// kubernetesCoordinator implements Coordinator using coordination.k8s.io Leases.
// Leases are the cluster-native primitive for this, so an installation that
// already runs the operator needs no extra etcd dependency to be highly
// available.
type kubernetesCoordinator struct {
	client   kubernetes.Interface
	config   KubernetesConfig
	mu       sync.Mutex
	canceled map[string]context.CancelFunc
	closed   bool
}

// NewKubernetes builds a Lease-based coordinator.
func NewKubernetes(cfg KubernetesConfig) (Coordinator, error) {
	resolved, err := cfg.withDefaults()
	if err != nil {
		return nil, err
	}
	client := resolved.Client
	if client == nil {
		inCluster, err := inClusterClient()
		if err != nil {
			return nil, err
		}
		client = inCluster
	}
	return &kubernetesCoordinator{
		client:   client,
		config:   resolved,
		canceled: make(map[string]context.CancelFunc),
	}, nil
}

const (
	// leaseNamePrefix namespaces OrbitJob leases so a shared namespace cannot
	// collide with unrelated controllers.
	leaseNamePrefix = "orbitjob-"
)

func (k *kubernetesCoordinator) Campaign(ctx context.Context, electionName string) (context.Context, error) {
	if electionName == "" {
		return nil, errors.New("election name is required")
	}
	leaderCtx, cancel := context.WithCancel(ctx)

	k.mu.Lock()
	if k.closed {
		k.mu.Unlock()
		cancel()
		return nil, errors.New("coordinator is closed")
	}
	k.canceled[electionName] = cancel
	k.mu.Unlock()

	if err := k.acquire(leaderCtx, electionName); err != nil {
		cancel()
		return nil, err
	}

	go func() {
		defer cancel()
		k.renewLoop(leaderCtx, electionName)
	}()
	return leaderCtx, nil
}

// acquire polls until this instance holds the Lease or ctx ends. Losing the race
// is expected, not an error: the caller wants leadership, not an immediate
// verdict on whether it is available.
func (k *kubernetesCoordinator) acquire(ctx context.Context, electionName string) error {
	name := leaseNamePrefix + electionName
	for {
		acquired, err := k.tryAcquire(ctx, name)
		if err == nil && acquired {
			return nil
		}
		if err != nil && !apierrors.IsConflict(err) {
			return fmt.Errorf("acquire lease %s: %w", name, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(k.config.RetryPeriod):
		}
	}
}

func (k *kubernetesCoordinator) tryAcquire(ctx context.Context, name string) (bool, error) {
	now := metav1.NewMicroTime(time.Now().UTC())
	holder := k.config.Identity
	leaseDuration := int32(k.config.LeaseDuration.Seconds())

	existing, err := k.client.CoordinationV1().Leases(k.config.Namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, createErr := k.client.CoordinationV1().Leases(k.config.Namespace).Create(ctx, &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: k.config.Namespace},
			Spec: coordinationv1.LeaseSpec{
				HolderIdentity:       &holder,
				LeaseDurationSeconds: &leaseDuration,
				AcquireTime:          &now,
				RenewTime:            &now,
			},
		}, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(createErr) {
			return false, nil
		}
		return createErr == nil, createErr
	}
	if err != nil {
		return false, err
	}

	if !leaseIsFree(existing, time.Now().UTC(), k.config.Identity) {
		return false, nil
	}
	// Optimistic concurrency: the resource version guard means only one of
	// several contenders can win even when they all observe a free lease.
	existing.Spec.HolderIdentity = &holder
	existing.Spec.LeaseDurationSeconds = &leaseDuration
	existing.Spec.AcquireTime = &now
	existing.Spec.RenewTime = &now
	if _, err := k.client.CoordinationV1().Leases(k.config.Namespace).Update(ctx, existing, metav1.UpdateOptions{}); err != nil {
		return false, err
	}
	return true, nil
}

// leaseIsFree reports whether the lease can be taken: it has never been held, the
// recorded holder is this instance, or the previous holder's lease has expired.
func leaseIsFree(lease *coordinationv1.Lease, now time.Time, identity string) bool {
	if lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity == "" {
		return true
	}
	if *lease.Spec.HolderIdentity == identity {
		return true
	}
	if lease.Spec.RenewTime == nil {
		return true
	}
	duration := kDefaultLeaseDuration
	if lease.Spec.LeaseDurationSeconds != nil {
		duration = time.Duration(*lease.Spec.LeaseDurationSeconds) * time.Second
	}
	return now.After(lease.Spec.RenewTime.Add(duration))
}

const kDefaultLeaseDuration = 15 * time.Second

// renewLoop keeps the lease alive until ctx ends or renewal fails past the
// deadline. Returning cancels the leader context, so a leader that cannot reach
// the API server stops acting as leader rather than working from stale authority.
func (k *kubernetesCoordinator) renewLoop(ctx context.Context, electionName string) {
	name := leaseNamePrefix + electionName
	deadline := time.Now().Add(k.config.RenewDeadline)
	ticker := time.NewTicker(k.config.RetryPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ok, err := k.tryAcquire(ctx, name)
			if err == nil && ok {
				deadline = time.Now().Add(k.config.RenewDeadline)
				continue
			}
			if time.Now().After(deadline) {
				return
			}
		}
	}
}

// TryLock converts the lease into a non-blocking mutex: it returns ErrLocked
// immediately when another holder owns the lease.
func (k *kubernetesCoordinator) TryLock(ctx context.Context, lockName string) (UnlockFunc, error) {
	if lockName == "" {
		return nil, errors.New("lock name is required")
	}
	acquired, err := k.tryAcquire(ctx, leaseNamePrefix+lockName)
	if err != nil {
		return nil, fmt.Errorf("acquire lock %s: %w", lockName, err)
	}
	if !acquired {
		return nil, ErrLocked
	}
	var once sync.Once
	return func() error {
		once.Do(func() {
			_ = k.client.CoordinationV1().Leases(k.config.Namespace).Delete(
				context.WithoutCancel(ctx), leaseNamePrefix+lockName, metav1.DeleteOptions{})
		})
		return nil
	}, nil
}

func (k *kubernetesCoordinator) Close() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.closed {
		return nil
	}
	k.closed = true
	for _, cancel := range k.canceled {
		cancel()
	}
	k.canceled = make(map[string]context.CancelFunc)
	return nil
}

// inClusterClient builds a clientset from the pod's service account. It is a
// variable so tests can substitute a fake without a cluster.
var inClusterClient = func() (kubernetes.Interface, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("in-cluster config: %w", err)
	}
	return kubernetes.NewForConfig(cfg)
}
