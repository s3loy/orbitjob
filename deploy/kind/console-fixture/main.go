// Command console-fixture provisions guarded, test-only Function definitions.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/lib/pq"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const fixtureContext = "kind-orbitjob-console-e2e"
const fixtureNamespace = "orbitjob"
const ownerLabel = "orbitjob.io/console-fixture-owner"
const caseLabel = "orbitjob.io/console-fixture-case"

type options struct {
	command, context, namespace, manifest, tenant, foreignTenant, group, image string
	timeout                                                                    time.Duration
}

type environment struct {
	db      *sql.DB
	kube    kubernetes.Interface
	dynamic dynamic.Interface
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "console-fixture:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("use seed, wait, status, cleanup, credentials, or revoke-credentials; add -h for options")
	}
	o := options{command: args[0]}
	f := flag.NewFlagSet("console-fixture "+o.command, flag.ContinueOnError)
	f.StringVar(&o.context, "context", fixtureContext, "required dedicated kind context")
	f.StringVar(&o.namespace, "namespace", fixtureNamespace, "required fixture namespace")
	f.StringVar(&o.manifest, "manifest", ".runtime/console-kind/m4-functions.json", "private ownership manifest")
	f.StringVar(&o.tenant, "tenant", "", "existing primary tenant ID (seed only)")
	f.StringVar(&o.foreignTenant, "foreign-tenant", "", "existing second tenant ID (seed only)")
	f.StringVar(&o.group, "group", "", "existing resource group in the primary tenant (seed only)")
	f.StringVar(&o.image, "image", "", "fixture image; otherwise read console-jobs.json")
	f.DurationVar(&o.timeout, "timeout", 2*time.Minute, "bounded revision or cancellation wait")
	if err := f.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if f.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if o.command != "seed" && o.command != "wait" && o.command != "status" && o.command != "cleanup" && o.command != "credentials" && o.command != "revoke-credentials" {
		return errors.New("unknown fixture command")
	}
	if err := validateGuard(o, os.Getenv("ORBITJOB_CONSOLE_FIXTURE"), os.Getenv("KUBECONFIG")); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(o.manifest), 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(o.manifest+".lock", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("manifest is locked; remove its .lock only after checking no helper is running")
	}
	_ = lock.Close()
	defer func() { _ = os.Remove(o.manifest + ".lock") }()
	ctx, cancel := context.WithTimeout(context.Background(), o.timeout+30*time.Second)
	defer cancel()
	env, closeEnv, err := connect(ctx, o)
	if err != nil {
		return err
	}
	defer closeEnv()
	if o.command == "credentials" {
		return seedCredentials(ctx, env, o)
	}
	if o.command == "revoke-credentials" {
		return revokeCredentials(ctx, o)
	}
	m, err := loadManifest(o.manifest)
	if err != nil {
		return err
	}
	if o.command == "seed" {
		return seed(ctx, env, o, m)
	}
	if m == nil {
		return errors.New("ownership manifest does not exist; seed first")
	}
	if err := verifyOwned(ctx, env.db, m); err != nil {
		return err
	}
	switch o.command {
	case "wait":
		return waitRevisions(ctx, env.db, o, m)
	case "status":
		return printStatus(ctx, env.db, m)
	case "cleanup":
		return cleanup(ctx, env, o, m)
	}
	return nil
}

func validateGuard(o options, guard, kubeconfig string) error {
	if guard != "1" {
		return errors.New("set ORBITJOB_CONSOLE_FIXTURE=1 explicitly")
	}
	if o.context != fixtureContext || o.namespace != fixtureNamespace {
		return errors.New("only kind-orbitjob-console-e2e / namespace orbitjob is permitted")
	}
	if kubeconfig == "" || strings.ContainsRune(kubeconfig, os.PathListSeparator) {
		return errors.New("KUBECONFIG must name one explicit dedicated kubeconfig file")
	}
	if o.timeout < time.Second || o.timeout > 10*time.Minute {
		return errors.New("timeout must be 1s..10m")
	}
	return nil
}

func connect(ctx context.Context, o options) (*environment, func(), error) {
	current, err := exec.CommandContext(ctx, "kubectl", "config", "current-context").Output()
	if err != nil || strings.TrimSpace(string(current)) != fixtureContext {
		return nil, nil, errors.New("current kubeconfig context must be kind-orbitjob-console-e2e")
	}
	listing, err := exec.CommandContext(ctx, "kind", "get", "clusters").Output()
	if err != nil || !containsLine(string(listing), "orbitjob-console-e2e") {
		return nil, nil, errors.New("the dedicated local kind cluster is not present")
	}
	raw, err := exec.CommandContext(ctx, "kubectl", "--context", o.context, "config", "view", "--minify", "--flatten", "--raw", "-o", "json").Output()
	if err != nil {
		return nil, nil, errors.New("cannot read the dedicated kubeconfig")
	}
	cfg, err := fixtureClientConfig(raw)
	if err != nil {
		return nil, nil, err
	}
	kube, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, nil, errors.New("cannot build fixture Kubernetes client")
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, nil, errors.New("cannot build fixture dynamic client")
	}
	nodes, err := kube.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil || len(nodes.Items) == 0 {
		return nil, nil, errors.New("cannot verify dedicated kind nodes")
	}
	for _, node := range nodes.Items {
		if !strings.HasPrefix(node.Name, "orbitjob-console-e2e-") {
			return nil, nil, errors.New("refusing a node outside the dedicated cluster")
		}
	}
	if _, err := kube.CoreV1().Namespaces().Get(ctx, o.namespace, metav1.GetOptions{}); err != nil {
		return nil, nil, errors.New("fixture namespace is unavailable")
	}
	secret, err := kube.CoreV1().Secrets(o.namespace).Get(ctx, "orbitjob-database", metav1.GetOptions{})
	if err != nil {
		return nil, nil, errors.New("cannot read the installation database Secret")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	dsn, err := localAdminDSN(string(secret.Data["admin-dsn"]), port)
	if err != nil {
		return nil, nil, err
	}
	forwardCtx, stopForward := context.WithCancel(ctx)
	forward := exec.CommandContext(forwardCtx, "kubectl", "--context", o.context, "-n", o.namespace,
		"port-forward", "--address=127.0.0.1", "service/orbitjob-postgres", fmt.Sprintf("%d:5432", port))
	if err := forward.Start(); err != nil {
		stopForward()
		return nil, nil, errors.New("cannot start database port-forward")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		stopForward()
		_ = forward.Wait()
		return nil, nil, errors.New("cannot open fixture admin database")
	}
	closeEnv := func() { _ = db.Close(); stopForward(); _ = forward.Wait() }
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	for {
		pingCtx, cancel := context.WithTimeout(ctx, time.Second)
		err := db.PingContext(pingCtx)
		cancel()
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			closeEnv()
			return nil, nil, ctx.Err()
		case <-deadline.C:
			closeEnv()
			return nil, nil, errors.New("fixture database login or port-forward did not become ready")
		case <-time.After(200 * time.Millisecond):
		}
	}
	var role string
	if err := db.QueryRowContext(ctx, "SELECT current_user").Scan(&role); err != nil || role != "orbitjob_admin" {
		closeEnv()
		return nil, nil, errors.New("fixture connection must use orbitjob_admin")
	}
	return &environment{db: db, kube: kube, dynamic: dyn}, closeEnv, nil
}

func localAdminDSN(secret string, port int) (string, error) {
	u, err := url.Parse(secret)
	if err != nil || u == nil || u.User == nil {
		return "", errors.New("installation admin DSN is invalid")
	}
	host := u.Hostname()
	allowed := host == "orbitjob-postgres.orbitjob.svc" || host == "orbitjob-postgres.orbitjob.svc.cluster.local"
	if (u.Scheme != "postgres" && u.Scheme != "postgresql") || !allowed || u.Port() != "5432" || u.Path != "/orbitjob" || u.User.Username() != "orbitjob_admin" {
		return "", errors.New("admin DSN must target this cluster's orbitjob PostgreSQL Service as orbitjob_admin")
	}
	// Rebuild rather than carry query options that could override host/user/db.
	local := url.URL{Scheme: "postgres", User: u.User, Host: net.JoinHostPort("127.0.0.1", fmt.Sprint(port)), Path: "/orbitjob", RawQuery: "connect_timeout=2&sslmode=disable"}
	return local.String(), nil
}

func containsLine(text, wanted string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == wanted {
			return true
		}
	}
	return false
}

// kind uses embedded client certificates. Other authentication is refused;
// the raw kubeconfig stays in memory and is never copied to the manifest.
func fixtureClientConfig(raw []byte) (*rest.Config, error) {
	var config struct {
		Current  string `json:"current-context"`
		Clusters []struct {
			Cluster struct {
				Server   string `json:"server"`
				CA       []byte `json:"certificate-authority-data"`
				Insecure bool   `json:"insecure-skip-tls-verify"`
			} `json:"cluster"`
		} `json:"clusters"`
		Users []struct {
			User struct {
				Certificate []byte `json:"client-certificate-data"`
				Key         []byte `json:"client-key-data"`
			} `json:"user"`
		} `json:"users"`
	}
	if err := json.Unmarshal(raw, &config); err != nil || config.Current != fixtureContext || len(config.Clusters) != 1 || len(config.Users) != 1 {
		return nil, errors.New("dedicated kind kubeconfig is invalid")
	}
	c, u := config.Clusters[0].Cluster, config.Users[0].User
	server, err := url.Parse(c.Server)
	if err != nil || server.Scheme != "https" || (server.Hostname() != "127.0.0.1" && server.Hostname() != "localhost" && server.Hostname() != "::1") || c.Insecure || len(c.CA) == 0 || len(u.Certificate) == 0 || len(u.Key) == 0 {
		return nil, errors.New("expected a verified loopback kind API server with embedded client certificates")
	}
	return &rest.Config{Host: c.Server, TLSClientConfig: rest.TLSClientConfig{CAData: c.CA, CertData: u.Certificate, KeyData: u.Key}, Timeout: 15 * time.Second}, nil
}

func writeManifest(path string, m *manifest) error {
	return writePrivateJSON(path, m)
}

func writePrivateJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".m4-functions-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(append(data, '\n'))
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
