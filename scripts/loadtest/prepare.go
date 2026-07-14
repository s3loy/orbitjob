package main

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type kubeObject struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name      string `yaml:"name"`
		Namespace string `yaml:"namespace"`
	} `yaml:"metadata"`
	Spec map[string]any `yaml:"spec"`
}

func ValidateLoadManifests(namespacePath, rbacPath string) error {
	data, err := os.ReadFile(namespacePath)
	if err != nil {
		return err
	}
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	quotas := map[string]map[string]string{}
	for {
		var object kubeObject
		if err := decoder.Decode(&object); err != nil {
			break
		}
		if object.Kind != "ResourceQuota" {
			continue
		}
		hard, _ := object.Spec["hard"].(map[string]any)
		values := map[string]string{}
		for key, value := range hard {
			values[key] = fmt.Sprint(value)
		}
		quotas[object.Metadata.Namespace] = values
	}
	if quotas["orbitjob-load"]["limits.cpu"] != "1" || quotas["orbitjob-load"]["limits.memory"] != "1Gi" {
		return fmt.Errorf("orbitjob-load quota does not match 1 CPU / 1Gi")
	}
	if quotas["orbitjob-tasks"]["limits.cpu"] != "5" || quotas["orbitjob-tasks"]["limits.memory"] != "4Gi" {
		return fmt.Errorf("orbitjob-tasks quota does not match 5 CPU / 4Gi")
	}
	rbac, err := os.ReadFile(rbacPath)
	if err != nil {
		return err
	}
	if strings.Contains(string(rbac), "\"secrets\"") || strings.Contains(string(rbac), "- secrets") {
		return fmt.Errorf("operations RBAC must not access secrets")
	}
	return nil
}
