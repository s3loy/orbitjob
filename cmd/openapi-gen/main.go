package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	yaml "github.com/goccy/go-yaml"

	adminhttp "orbitjob/internal/admin/http"
)

func main() {
	var outPath string
	var check bool

	flag.StringVar(&outPath, "out", "api/openapi.yaml", "path to write OpenAPI YAML")
	flag.BoolVar(&check, "check", false, "fail when generated OpenAPI YAML differs from checked-in file")
	flag.Parse()

	rendered, err := renderOpenAPIYAML(adminhttp.ServiceOpenAPIDocument())
	if err != nil {
		fmt.Fprintf(os.Stderr, "render openapi yaml: %v\n", err)
		os.Exit(1)
	}

	outPath = filepath.Clean(outPath)
	if check {
		if err := verifySpec(outPath, rendered); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("openapi is up to date: %s\n", outPath)
		return
	}

	if err := writeSpec(outPath, rendered); err != nil {
		fmt.Fprintf(os.Stderr, "write openapi yaml: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("wrote openapi yaml: %s\n", outPath)
}

func renderOpenAPIYAML(doc adminhttp.OpenAPIDocument) ([]byte, error) {
	jsonBytes, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal openapi json: %w", err)
	}

	yamlBytes, err := yaml.JSONToYAML(jsonBytes)
	if err != nil {
		return nil, fmt.Errorf("convert json to yaml: %w", err)
	}

	if len(yamlBytes) == 0 || yamlBytes[len(yamlBytes)-1] != '\n' {
		yamlBytes = append(yamlBytes, '\n')
	}

	return yamlBytes, nil
}

func verifySpec(path string, generated []byte) error {
	existing, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	if !bytes.Equal(existing, generated) {
		return fmt.Errorf("openapi spec drift detected, regenerate with: go run ./cmd/openapi-gen -out %s", path)
	}

	return nil
}

func writeSpec(path string, generated []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	if err := os.WriteFile(path, generated, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	return nil
}
