package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/yaml"
)

func TestAdminEntryChartRoutes(t *testing.T) {
	for _, tc := range []struct {
		name        string
		values      string
		serviceType corev1.ServiceType
		port        int32
		nodePort    int32
		ingress     bool
		host        string
		className   string
		tlsSecret   string
		annotations map[string]string
	}{
		{name: "existing installation defaults", values: `{}`, serviceType: corev1.ServiceTypeClusterIP, port: 8080},
		{name: "custom service port", values: `{"service":{"port":9080}}`, serviceType: corev1.ServiceTypeClusterIP, port: 9080},
		{name: "allocated node port", values: `{"service":{"type":"NodePort"}}`, serviceType: corev1.ServiceTypeNodePort, port: 8080},
		{name: "explicit node port", values: `{"service":{"type":"NodePort","port":80,"nodePort":31080}}`, serviceType: corev1.ServiceTypeNodePort, port: 80, nodePort: 31080},
		{name: "load balancer allocation", values: `{"service":{"type":"LoadBalancer","annotations":{"example.com/scope":"internal"}}}`, serviceType: corev1.ServiceTypeLoadBalancer, port: 8080, annotations: map[string]string{"example.com/scope": "internal"}},
		{name: "custom cluster node port range", values: `{"service":{"type":"LoadBalancer","nodePort":28080}}`, serviceType: corev1.ServiceTypeLoadBalancer, port: 8080, nodePort: 28080},
		{name: "hostname ingress", values: `{"ingress":{"enabled":true,"host":"orbitjob.example.com"}}`, serviceType: corev1.ServiceTypeClusterIP, port: 8080, ingress: true, host: "orbitjob.example.com"},
		{name: "hostless ingress", values: `{"ingress":{"enabled":true}}`, serviceType: corev1.ServiceTypeClusterIP, port: 8080, ingress: true},
		{name: "hostname TLS and custom backend", values: `{"service":{"port":9080},"ingress":{"enabled":true,"className":"existing-gateway","host":"orbitjob.example.com","annotations":{"example.com/policy":"checked"},"tls":{"enabled":true,"secretName":"orbitjob-tls"}}}`, serviceType: corev1.ServiceTypeClusterIP, port: 9080, ingress: true, host: "orbitjob.example.com", className: "existing-gateway", tlsSecret: "orbitjob-tls"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := adminEntryHelm(t, "template", tc.values)
			if err != nil {
				t.Fatalf("render chart: %v\n%s", err, data)
			}
			service, ingress, deployment := decodeAdminEntry(t, data)
			if service.Namespace != "entry-install" || service.Spec.Type != tc.serviceType {
				t.Fatalf("Service namespace/type: %s/%s", service.Namespace, service.Spec.Type)
			}
			if !reflect.DeepEqual(service.Annotations, tc.annotations) {
				t.Fatalf("Service annotations: got %v, want %v", service.Annotations, tc.annotations)
			}
			if len(service.Spec.Ports) != 1 {
				t.Fatalf("unexpected Service ports: %+v", service.Spec.Ports)
			}
			port := service.Spec.Ports[0]
			if port.Name != "http" || port.Port != tc.port || port.NodePort != tc.nodePort || port.TargetPort != intstr.FromString("health") {
				t.Fatalf("Service endpoint: %+v", port)
			}
			if !reflect.DeepEqual(service.Spec.Selector, deployment.Spec.Template.Labels) {
				t.Fatalf("Service does not select the Admin API deployment: %v vs %v", service.Spec.Selector, deployment.Spec.Template.Labels)
			}
			containers := deployment.Spec.Template.Spec.Containers
			if len(containers) != 1 || len(containers[0].Ports) != 1 || containers[0].Ports[0].Name != "health" || containers[0].Ports[0].ContainerPort != 8080 {
				t.Fatalf("custom Service port changed the process listener: %+v", containers)
			}
			if !tc.ingress {
				if ingress != nil {
					t.Fatal("default installation unexpectedly publishes an Ingress")
				}
				return
			}
			if ingress == nil || ingress.Namespace != "entry-install" || ingress.Spec.DefaultBackend != nil || len(ingress.Spec.Rules) != 1 {
				t.Fatalf("unexpected Ingress: %+v", ingress)
			}
			if tc.className == "" {
				if ingress.Spec.IngressClassName != nil {
					t.Fatal("empty className must let the cluster choose its default")
				}
			} else if ingress.Spec.IngressClassName == nil || *ingress.Spec.IngressClassName != tc.className {
				t.Fatalf("Ingress class: %v", ingress.Spec.IngressClassName)
			}
			rule := ingress.Spec.Rules[0]
			if rule.Host != tc.host || rule.HTTP == nil || len(rule.HTTP.Paths) != 4 {
				t.Fatalf("unexpected host or routes: %+v", rule)
			}
			expectedRoutes := map[string]networkingv1.PathType{
				"/":             networkingv1.PathTypeExact,
				"/console":      networkingv1.PathTypePrefix,
				"/api/v1":       networkingv1.PathTypePrefix,
				"/openapi.json": networkingv1.PathTypeExact,
			}
			for _, path := range rule.HTTP.Paths {
				typ, ok := expectedRoutes[path.Path]
				if !ok || path.PathType == nil || *path.PathType != typ {
					t.Fatalf("Ingress admits an unexpected public route: %+v", path)
				}
				delete(expectedRoutes, path.Path)
				backend := path.Backend.Service
				if backend == nil || path.Backend.Resource != nil || backend.Name != service.Name || backend.Port.Name != "" || backend.Port.Number != tc.port {
					t.Fatalf("Ingress does not preserve the Console/API origin: %+v", path.Backend)
				}
			}
			if len(expectedRoutes) != 0 {
				t.Fatalf("missing public routes: %v", expectedRoutes)
			}
			if tc.tlsSecret == "" {
				if len(ingress.Spec.TLS) != 0 {
					t.Fatalf("TLS appeared without configuration: %+v", ingress.Spec.TLS)
				}
			} else {
				want := []networkingv1.IngressTLS{{Hosts: []string{tc.host}, SecretName: tc.tlsSecret}}
				if !reflect.DeepEqual(ingress.Spec.TLS, want) || ingress.Annotations["example.com/policy"] != "checked" {
					t.Fatalf("Ingress lost TLS/annotations: %+v", ingress)
				}
			}
		})
	}
}

func TestAdminEntryChartRejectsInvalidConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values string
	}{
		{"unsupported service type", `{"service":{"type":"ExternalName"}}`},
		{"zero service port", `{"service":{"port":0}}`},
		{"large service port", `{"service":{"port":65536}}`},
		{"noninteger service port", `{"service":{"port":"8080"}}`},
		{"node port on internal service", `{"service":{"nodePort":31080}}`},
		{"zero node port", `{"service":{"type":"NodePort","nodePort":0}}`},
		{"large node port", `{"service":{"type":"NodePort","nodePort":65536}}`},
		{"nonstring annotation", `{"service":{"annotations":{"example.com/enabled":true}}}`},
		{"unsupported service field", `{"service":{"ports":[80]}}`},
		{"IP rule host", `{"ingress":{"enabled":true,"host":"203.0.113.10"}}`},
		{"IPv6 rule host", `{"ingress":{"enabled":true,"host":"2001:db8::1"}}`},
		{"URL rule host", `{"ingress":{"host":"https://orbitjob.example.com"}}`},
		{"port in rule host", `{"ingress":{"host":"orbitjob.example.com:8443"}}`},
		{"oversized DNS label", `{"ingress":{"host":"` + strings.Repeat("a", 64) + `.example.com"}}`},
		{"invalid Ingress class", `{"ingress":{"className":"invalid_class"}}`},
		{"public catchall override", `{"ingress":{"enabled":true,"paths":["/"]}}`},
		{"TLS missing secret", `{"ingress":{"enabled":true,"host":"orbitjob.example.com","tls":{"enabled":true}}}`},
		{"TLS missing host", `{"ingress":{"enabled":true,"tls":{"enabled":true,"secretName":"orbitjob-tls"}}}`},
		{"TLS on disabled ingress", `{"ingress":{"host":"orbitjob.example.com","tls":{"enabled":true,"secretName":"orbitjob-tls"}}}`},
		{"TLS secret while disabled", `{"ingress":{"tls":{"secretName":"orbitjob-tls"}}}`},
		{"invalid TLS secret name", `{"ingress":{"enabled":true,"host":"orbitjob.example.com","tls":{"enabled":true,"secretName":"invalid_secret"}}}`},
		{"independent TLS hosts override", `{"ingress":{"tls":{"hosts":["other.example.com"]}}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := adminEntryHelm(t, "template", tc.values)
			if err == nil || !strings.Contains(string(out), "adminapi") || !strings.Contains(string(out), "schema") {
				t.Fatalf("invalid entry configuration did not fail schema validation: %v\n%s", err, out)
			}
		})
	}
}

func TestAdminEntryChartInstallNotes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		values   string
		contains []string
	}{
		{"internal", `{}`, []string{"Service is internal to the cluster", "kubectl -n entry-install port-forward service/orbitjob-admin-api 8080:8080", "Collector state:", "Execution networking:"}},
		{"hostname TLS", `{"service":{"port":9080},"ingress":{"enabled":true,"host":"orbitjob.example.com","tls":{"enabled":true,"secretName":"orbitjob-tls"}}}`, []string{"Console URL: https://orbitjob.example.com/console/", "existing Secret orbitjob-tls in namespace entry-install", "8080:9080", "/metrics and /healthz are excluded"}},
		{"hostless gateway", `{"ingress":{"enabled":true}}`, []string{"get ingress orbitjob-admin-api", ".status.loadBalancer.ingress[*]", "http://<gateway-address>/console/", "does not configure TLS"}},
		{"load balancer", `{"service":{"type":"LoadBalancer","port":9080}}`, []string{"get service orbitjob-admin-api", ".status.loadBalancer.ingress[*]", "http://<load-balancer-address>:9080/console/", "including /metrics and /healthz", "no address is available from Kubernetes yet"}},
		{"node port", `{"service":{"type":"NodePort"}}`, []string{"allocated node port", ".nodePort}", "http://<reachable-node-address>:<node-port>/console/", "does not assign a public node address", "including /metrics and /healthz"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := adminEntryHelm(t, "install", tc.values)
			if err != nil {
				t.Fatalf("client-side installation: %v\n%s", err, out)
			}
			_, notes, found := strings.Cut(string(out), "NOTES:\n")
			if !found {
				t.Fatalf("installation did not print notes: %s", out)
			}
			for _, want := range tc.contains {
				if !strings.Contains(notes, want) {
					t.Errorf("installation notes missing %q:\n%s", want, notes)
				}
			}
		})
	}
}

func adminEntryHelm(t *testing.T, operation, adminapi string) ([]byte, error) {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("Helm is required by the separate Kubernetes quality gate")
	}
	var values json.RawMessage
	if err := json.Unmarshal([]byte(adminapi), &values); err != nil {
		t.Fatal(err)
	}
	contents, err := json.Marshal(map[string]any{
		"operator": map[string]string{"namespaceTenants": "tasks=01ARZ3NDEKTSV4RRFFQ69G5FAV"},
		"adminapi": values,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "values.json")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{operation, "entry-test", "../charts/orbitjob", "--namespace", "entry-install", "-f", path}
	if operation == "install" {
		args = append(args, "--dry-run=client")
	}
	return exec.Command("helm", args...).CombinedOutput()
}

func decodeAdminEntry(t *testing.T, data []byte) (corev1.Service, *networkingv1.Ingress, appsv1.Deployment) {
	t.Helper()
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	var service corev1.Service
	var ingress *networkingv1.Ingress
	var deployment appsv1.Deployment
	for {
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		var meta struct {
			metav1.TypeMeta `json:",inline"`
			Metadata        metav1.ObjectMeta `json:"metadata"`
		}
		if err := json.Unmarshal(raw, &meta); err != nil {
			t.Fatal(err)
		}
		if meta.Metadata.Name != "orbitjob-admin-api" {
			continue
		}
		switch meta.Kind {
		case "Service":
			if service.Name != "" {
				t.Fatal("duplicate Admin API Service")
			}
			if err := json.Unmarshal(raw, &service); err != nil {
				t.Fatal(err)
			}
		case "Ingress":
			if ingress != nil {
				t.Fatal("duplicate Admin API Ingress")
			}
			ingress = &networkingv1.Ingress{}
			if err := json.Unmarshal(raw, ingress); err != nil {
				t.Fatal(err)
			}
		case "Deployment":
			if err := json.Unmarshal(raw, &deployment); err != nil {
				t.Fatal(err)
			}
		}
	}
	if service.Name == "" || deployment.Name == "" {
		t.Fatal("missing Admin API Service or Deployment")
	}
	return service, ingress, deployment
}
