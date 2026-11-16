package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreflightChecksDesktopHostStorage(t *testing.T) {
	for _, tc := range []struct {
		name, operatingSystem, endpoint, hostFree, diskPath, diagnostic string
		wantSuccess                                                     bool
	}{
		{"native daemon", "Docker Engine", "unix:///docker.sock", "1", "missing", "Docker profile", true},
		{"desktop sufficient", "Docker Desktop", "unix:///docker.sock", "41943040", "present", "host storage", true},
		{"desktop host exhausted", "Docker Desktop", "unix:///docker.sock", "41943039", "present", "guest free space is insufficient evidence", false},
		{"desktop missing disk", "Docker Desktop", "unix:///docker.sock", "90000000", "missing", "ORBITJOB_DOCKER_STORAGE_PATH", false},
		{"desktop unreadable capacity", "Docker Desktop", "unix:///docker.sock", "unknown", "present", "host storage needs", false},
		{"remote desktop", "Docker Desktop", "ssh://remote", "90000000", "present", "remote Docker host", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			fakebin := filepath.Join(dir, "bin")
			for _, name := range []string{"kubectl", "helm", "go", "nc"} {
				writeGateExecutable(t, filepath.Join(fakebin, name), "#!/bin/sh\nexit 0\n")
			}
			writeGateExecutable(t, filepath.Join(fakebin, "kind"), "#!/bin/sh\necho orbitjob-dev-control-plane\n")
			writeGateExecutable(t, filepath.Join(fakebin, "docker"), `#!/bin/sh
case "$*" in
  *NCPU*) echo '4 8589934592' ;;
  *OperatingSystem*) echo "$TEST_DOCKER_OS" ;;
  'context inspect'*) echo "$TEST_DOCKER_ENDPOINT" ;;
  exec*) printf 'Filesystem 1024-blocks Used Available Capacity Mounted\nvm 999999999 1 90000000 1%% /var/lib\n' ;;
  *) exit 1 ;;
esac
`)
			writeGateExecutable(t, filepath.Join(fakebin, "df"), `#!/bin/sh
printf 'Filesystem 1024-blocks Used Available Capacity Mounted\nhost 999999999 1 %s 1%% /data\n' "$TEST_HOST_FREE"
`)
			disk := filepath.Join(dir, "Docker.raw")
			if tc.diskPath == "present" {
				if err := os.WriteFile(disk, nil, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("bash", "preflight-local.sh")
			cmd.Env = append(gateEnvironment("PATH", "ORBITJOB_DOCKER_STORAGE_PATH", "TEST_DOCKER_OS", "TEST_DOCKER_ENDPOINT", "TEST_HOST_FREE"),
				"PATH="+fakebin+":"+os.Getenv("PATH"),
				"ORBITJOB_DOCKER_STORAGE_PATH="+disk,
				"TEST_DOCKER_OS="+tc.operatingSystem,
				"TEST_DOCKER_ENDPOINT="+tc.endpoint,
				"TEST_HOST_FREE="+tc.hostFree)
			out, err := cmd.CombinedOutput()
			if (err == nil) != tc.wantSuccess {
				t.Fatalf("success=%v, want %v:\n%s", err == nil, tc.wantSuccess, out)
			}
			if !strings.Contains(string(out), tc.diagnostic) {
				t.Fatalf("missing diagnostic %q:\n%s", tc.diagnostic, out)
			}
		})
	}
}

func TestPreflightClusterRequiresMatchingKubectlMinor(t *testing.T) {
	for _, tc := range []struct {
		name, client, server, claim, diagnostic string
		wantSuccess                             bool
	}{
		{"matched", "37", "37", "5Gi", "minor version match", true},
		{"development minor suffix", "37+", "37", "5Gi", "minor version match", true},
		{"old Docker bundled client", "34", "37", "5Gi", "requires kubectl 1.37.x", false},
		{"newer client", "38", "37", "5Gi", "requires kubectl 1.37.x", false},
		{"malformed client", "unknown", "37", "5Gi", "incomplete kubectl", false},
		{"missing server", "37", "", "5Gi", "incomplete kubectl", false},
		{"undersized database", "37", "37", "2Gi", "stop installation and prepare a separate backup and storage-migration plan", false},
		{"larger database", "37", "37", "6Gi", "database volume meets", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			fakebin := filepath.Join(dir, "bin")
			for _, name := range []string{"helm", "go", "nc"} {
				writeGateExecutable(t, filepath.Join(fakebin, name), "#!/bin/sh\nexit 0\n")
			}
			writeGateExecutable(t, filepath.Join(fakebin, "kind"), "#!/bin/sh\necho orbitjob-dev-control-plane\n")
			writeGateExecutable(t, filepath.Join(fakebin, "docker"), `#!/bin/sh
case "$*" in
  *NCPU*) echo '4 8589934592' ;;
  *OperatingSystem*) echo 'Docker Engine' ;;
  exec*) printf 'Filesystem 1024-blocks Used Available Capacity Mounted\nvm 999999999 1 90000000 1%% /var/lib\n' ;;
  *) exit 1 ;;
esac
`)
			writeGateExecutable(t, filepath.Join(fakebin, "kubectl"), `#!/bin/sh
case "$*" in
  *'version -o yaml'*) printf '%s\n' "$TEST_VERSIONS" ;;
  *'get deploy'*) echo current-database ;;
  *'get pvc'*) echo "$TEST_DATABASE_CLAIM" ;;
  *) exit 1 ;;
esac
`)
			versions := "clientVersion:\n  major: \"1\"\n  minor: \"" + tc.client + "\"\n"
			if tc.server != "" {
				versions += "serverVersion:\n  major: \"1\"\n  minor: \"" + tc.server + "\"\n"
			}
			cmd := exec.Command("bash", "preflight-local.sh", "--cluster")
			cmd.Env = append(gateEnvironment("PATH", "TEST_VERSIONS", "TEST_DATABASE_CLAIM"), "PATH="+fakebin+":"+os.Getenv("PATH"), "TEST_VERSIONS="+versions, "TEST_DATABASE_CLAIM="+tc.claim)
			out, err := cmd.CombinedOutput()
			if (err == nil) != tc.wantSuccess || !strings.Contains(string(out), tc.diagnostic) {
				t.Fatalf("success=%v, want %v with %q:\n%s", err == nil, tc.wantSuccess, tc.diagnostic, out)
			}
		})
	}
}
