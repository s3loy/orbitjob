package main

import (
	"strings"
	"testing"
)

func TestImageLockRequiresTagAndDigests(t *testing.T) {
	lock := ImageLock{SchemaVersion: "v020-images/v1", Images: []LockedImage{{
		Name: "python", Repository: "docker.io/library/python", Tag: "3.14-alpine", Purpose: []string{"json"},
	}}}
	err := ValidateImageLock(lock)
	if err == nil || !strings.Contains(err.Error(), "index digest") {
		t.Fatalf("error = %v", err)
	}
}

func TestImageReferenceIncludesReadableTagAndDigest(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	got := ImageReference(LockedImage{Repository: "docker.io/library/python", Tag: "3.14-alpine", IndexDigest: digest})
	want := "docker.io/library/python:3.14-alpine@" + digest
	if got != want {
		t.Fatalf("reference = %q, want %q", got, want)
	}
}
