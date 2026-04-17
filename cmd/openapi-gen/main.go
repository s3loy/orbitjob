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

var jsonToYAML = yaml.JSONToYAML

var renderOpenAPIYAMLFn = renderOpenAPIYAML

var verifySpecFn = verifySpec

var writeSpecFn = writeSpec

var exitFn = os.Exit

func main() {
	err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		exitFn(1)
	}
}

func run(args []string) error {
	var outPath string
	var check bool

	fs := flag.NewFlagSet("openapi-gen", flag.ContinueOnError)
	fs.StringVar(&outPath, "out", "api/openapi.yaml", "path to write OpenAPI YAML")
	fs.BoolVar(&check, "check", false, "fail when generated OpenAPI YAML differs from checked-in file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	rendered, err := renderOpenAPIYAMLFn(adminhttp.ServiceOpenAPIDocument())
	if err != nil {
		return fmt.Errorf("render openapi yaml: %w", err)
	}

	outPath = filepath.Clean(outPath)
	if check {
		if err := verifySpecFn(outPath, rendered); err != nil {
			return err
		}
		fmt.Printf("openapi is up to date: %s\n", outPath)
		return nil
	}

	if err := writeSpecFn(outPath, rendered); err != nil {
		return fmt.Errorf("write openapi yaml: %w", err)
	}
	fmt.Printf("wrote openapi yaml: %s\n", outPath)

	return nil
}

func renderOpenAPIYAML(doc adminhttp.OpenAPIDocument) ([]byte, error) {
	jsonBytes, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal openapi json: %w", err)
	}

	yamlBytes, err := jsonToYAML(jsonBytes)
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
