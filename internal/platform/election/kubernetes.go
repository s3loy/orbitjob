package election

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// KubernetesConfig configures ordinary client-go Lease election.
type KubernetesConfig struct {
	Namespace     string
	Identity      string
	LeaseDuration time.Duration
	RenewDeadline time.Duration
	RetryPeriod   time.Duration
	Client        kubernetes.Interface
}

func (c KubernetesConfig) withDefaults() (KubernetesConfig, error) {
	if c.Namespace == "" {
		return c, errors.New("kubernetes election namespace is required")
	}
	if c.Identity == "" {
		c.Identity = os.Getenv("POD_NAME")
	}
	if c.Identity == "" {
		var err error
		c.Identity, err = os.Hostname()
		if err != nil {
			return c, fmt.Errorf("derive election identity: %w", err)
		}
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
	// LeaseSpec encodes duration as whole seconds. Do not silently truncate a
	// smaller interval below the acting leader's renewal deadline.
	if c.LeaseDuration%time.Second != 0 || c.LeaseDuration/time.Second > 1<<31-1 {
		return c, errors.New("lease duration must fit whole int32 seconds")
	}
	// Keep the official validation (including retry jitter) authoritative.
	_, err := leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
		Lock: &resourcelock.LeaseLock{LockConfig: resourcelock.ResourceLockConfig{Identity: c.Identity}}, LeaseDuration: c.LeaseDuration,
		RenewDeadline: c.RenewDeadline, RetryPeriod: c.RetryPeriod,
		Callbacks: leaderelection.LeaderCallbacks{OnStartedLeading: func(context.Context) {}, OnStoppedLeading: func() {}},
	})
	return c, err
}

type leadershipSession struct {
	cancel  context.CancelFunc
	done    chan struct{}
	started atomic.Bool
}

type kubernetesCoordinator struct {
	client    kubernetes.Interface
	config    KubernetesConfig
	mu        sync.Mutex
	sessions  map[string]*leadershipSession
	closed    bool
	closeDone chan struct{}
}

var leaseSessions = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "orbitjob_kubernetes_leadership_sessions_total",
	Help: "Kubernetes leadership sessions by bounded lifecycle result (acquired, lost, canceled, rejected).",
}, []string{"result"})

var leaseRequests = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "orbitjob_kubernetes_lease_request_errors_total",
	Help: "Failed Kubernetes Lease requests by bounded operation (get, create, update).",
}, []string{"operation"})

// NewKubernetes exposes leadership, not the unsupported TTL mutex experiment.
func NewKubernetes(cfg KubernetesConfig) (Leadership, error) {
	resolved, err := cfg.withDefaults()
	if err != nil {
		return nil, err
	}
	client := resolved.Client
	if client == nil {
		client, err = inClusterClient()
		if err != nil {
			return nil, err
		}
	}
	return &kubernetesCoordinator{client: client, config: resolved,
		sessions: map[string]*leadershipSession{}, closeDone: make(chan struct{})}, nil
}

const leaseNamePrefix = "orbitjob-"

func (k *kubernetesCoordinator) Campaign(ctx context.Context, name string) (context.Context, error) {
	if name == "" {
		return nil, errors.New("election name is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sessionCtx, cancel := context.WithCancel(ctx)
	session := &leadershipSession{cancel: cancel, done: make(chan struct{})}
	started := make(chan context.Context, 1)
	k.mu.Lock()
	if k.closed || k.sessions[name] != nil {
		k.mu.Unlock()
		cancel()
		leaseSessions.WithLabelValues("rejected").Inc()
		return nil, errors.New("coordinator closed or election already campaigning")
	}
	k.sessions[name] = session
	k.mu.Unlock()
	identity := k.config.Identity + "_" + rand.Text()
	lock := &boundedLeaseLock{LeaseLock: &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{Name: leaseNamePrefix + name, Namespace: k.config.Namespace},
		Client:    k.client.CoordinationV1(), LockConfig: resourcelock.ResourceLockConfig{Identity: identity},
	}, budget: k.config.RenewDeadline}
	elector, err := leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
		Lock: lock, Name: leaseNamePrefix + name, LeaseDuration: k.config.LeaseDuration,
		RenewDeadline: k.config.RenewDeadline, RetryPeriod: k.config.RetryPeriod,
		// The caller drains its workers after this context is canceled. Releasing
		// here would allow a successor before that drain has completed.
		ReleaseOnCancel: false,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(leader context.Context) {
				if leader.Err() != nil || sessionCtx.Err() != nil {
					return
				}
				session.started.Store(true)
				leaseSessions.WithLabelValues("acquired").Inc()
				slog.Info("kubernetes leadership acquired", "election", name, "holder", identity)
				select {
				case started <- leader:
				case <-sessionCtx.Done():
				}
			},
			OnStoppedLeading: func() { cancel() },
		},
	})
	if err != nil {
		k.finishSession(name, session)
		return nil, err
	}
	go func() {
		elector.Run(sessionCtx)
		result := "canceled"
		if ctx.Err() == nil && session.started.Load() && !k.isClosed() {
			result = "lost"
		}
		leaseSessions.WithLabelValues(result).Inc()
		slog.Info("kubernetes leadership session stopped", "election", name, "holder", identity)
		k.finishSession(name, session)
	}()
	select {
	case leader := <-started:
		if err := sessionCtx.Err(); err != nil {
			return nil, err
		}
		if err := leader.Err(); err != nil {
			return nil, err
		}
		return leader, nil
	case <-session.done:
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, ErrSessionExpired
	case <-ctx.Done():
		cancel()
		<-session.done
		return nil, ctx.Err()
	}
}

func (k *kubernetesCoordinator) isClosed() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.closed
}

func (k *kubernetesCoordinator) finishSession(name string, s *leadershipSession) {
	s.cancel()
	k.mu.Lock()
	if k.sessions[name] == s {
		delete(k.sessions, name)
	}
	close(s.done)
	k.mu.Unlock()
}

func (k *kubernetesCoordinator) Close() error {
	k.mu.Lock()
	if k.closed {
		done := k.closeDone
		k.mu.Unlock()
		<-done
		return nil
	}
	k.closed = true
	sessions := make([]*leadershipSession, 0, len(k.sessions))
	for _, s := range k.sessions {
		sessions = append(sessions, s)
	}
	k.mu.Unlock()
	for _, s := range sessions {
		s.cancel()
	}
	for _, s := range sessions {
		<-s.done
	}
	close(k.closeDone)
	return nil
}

// Bound each API operation, including initial acquisition. client-go owns all
// record comparison and renewal logic; this adapter only bounds transport stalls.
type boundedLeaseLock struct {
	*resourcelock.LeaseLock
	budget time.Duration
}

func (l *boundedLeaseLock) Get(ctx context.Context) (*resourcelock.LeaderElectionRecord, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, l.budget)
	defer cancel()
	record, raw, err := l.LeaseLock.Get(ctx)
	if err != nil {
		leaseRequests.WithLabelValues("get").Inc()
	}
	return record, raw, err
}
func (l *boundedLeaseLock) Create(ctx context.Context, record resourcelock.LeaderElectionRecord) error {
	ctx, cancel := context.WithTimeout(ctx, l.budget)
	defer cancel()
	err := l.LeaseLock.Create(ctx, record)
	if err != nil {
		leaseRequests.WithLabelValues("create").Inc()
	}
	return err
}
func (l *boundedLeaseLock) Update(ctx context.Context, record resourcelock.LeaderElectionRecord) error {
	ctx, cancel := context.WithTimeout(ctx, l.budget)
	defer cancel()
	err := l.LeaseLock.Update(ctx, record)
	if err != nil {
		leaseRequests.WithLabelValues("update").Inc()
	}
	return err
}

var inClusterClient = func() (kubernetes.Interface, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("in-cluster config: %w", err)
	}
	return kubernetes.NewForConfig(cfg)
}
