package main

import (
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type ImageLock struct {
	SchemaVersion string        `yaml:"schema_version"`
	Images        []LockedImage `yaml:"images"`
}

type LockedImage struct {
	Name              string            `yaml:"name"`
	Repository        string            `yaml:"repository"`
	Tag               string            `yaml:"tag"`
	IndexDigest       string            `yaml:"index_digest"`
	PlatformManifests map[string]string `yaml:"platform_manifests"`
	Purpose           []string          `yaml:"purpose"`
}

func LoadImageLock(path string) (ImageLock, error) {
	file, err := os.Open(path)
	if err != nil {
		return ImageLock{}, err
	}
	defer func() { _ = file.Close() }()
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	var lock ImageLock
	if err := decoder.Decode(&lock); err != nil {
		return ImageLock{}, fmt.Errorf("decode image lock: %w", err)
	}
	return lock, ValidateImageLock(lock)
}

func ValidateImageLock(lock ImageLock) error {
	if lock.SchemaVersion != "v020-images/v1" {
		return fmt.Errorf("image lock schema_version must be v020-images/v1")
	}
	seen := map[string]bool{}
	allowed := map[string]bool{
		"docker.io/library/python": true, "docker.io/library/postgres": true,
		"docker.io/curlimages/curl": true, "docker.io/library/alpine": true,
		"docker.io/library/busybox": true, "registry.k8s.io/kubectl": true,
	}
	for _, image := range lock.Images {
		if image.Name == "" || seen[image.Name] {
			return fmt.Errorf("image names must be non-empty and unique")
		}
		seen[image.Name] = true
		if !allowed[image.Repository] {
			return fmt.Errorf("image %s uses unapproved repository %q", image.Name, image.Repository)
		}
		if image.Tag == "" || len(image.Purpose) == 0 {
			return fmt.Errorf("image %s requires tag and purpose", image.Name)
		}
		if !digestPattern.MatchString(image.IndexDigest) {
			return fmt.Errorf("image %s requires a valid index digest", image.Name)
		}
		for _, platform := range []string{"linux/amd64", "linux/arm64"} {
			if !digestPattern.MatchString(image.PlatformManifests[platform]) {
				return fmt.Errorf("image %s requires a valid %s manifest digest", image.Name, platform)
			}
		}
	}
	if len(lock.Images) != 6 {
		return fmt.Errorf("image lock must contain 6 entries, got %d", len(lock.Images))
	}
	return nil
}

func ImageReference(image LockedImage) string {
	return image.Repository + ":" + image.Tag + "@" + image.IndexDigest
}
