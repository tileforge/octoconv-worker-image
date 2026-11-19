package convert

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tileforge/octoconv-worker-image/internal/sandbox"
)

func TestImageGrantsMatchTheMeasuredSet(t *testing.T) {
	if strings.Join(imageReadOnly, "\n") != strings.Join([]string{
		"/lib",
		"/usr/lib",
		"/etc/ld.so.cache",
		"/proc/self/auxv",
		"/proc/self/status",
		"/proc/filesystems",
		"/proc/cpuinfo",
		"/sys/devices/system/cpu/online",
		"/sys/devices/system/cpu/possible",
	}, "\n") {
		t.Fatalf("read only %v", imageReadOnly)
	}
	if strings.Join(imageEngines, "\n") != "vips" {
		t.Fatalf("engines %v", imageEngines)
	}
	if imagePreflightOutside != "/var/tmp" || ImageSandboxDisabledEnv != "IMAGE_SANDBOX_DISABLED" {
		t.Fatal("outside fixture or disable env")
	}
	for _, banned := range []string{"/tmp", "/usr/share", "/etc/localtime", "/"} {
		for _, path := range imageReadOnly {
			if path == banned {
				t.Fatalf("read only contains %s", banned)
			}
		}
	}
}

func TestWithSandboxEmptyHelperIsUnconfined(t *testing.T) {
	c := &LibvipsConverter{}
	c.WithSandbox(nil)
	if c.sandbox.confines() {
		t.Fatal("confined")
	}
	proof, err := ImagePreflight(t.Context(), c)
	if err != nil || proof.OutputBytes != 0 {
		t.Fatalf("proof %+v err %v", proof, err)
	}
}

func TestForJobGrantsTheJobDirectoryAndVips(t *testing.T) {
	t.Setenv("TMPDIR", "/tmp")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "vips"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	job := filepath.Join(dir, "job")
	c := (&LibvipsConverter{}).WithSandbox([]string{"/usr/local/bin/image-worker", sandbox.ExecMode})
	spec := strings.Join(c.jobExec(job).spec, "\n")
	for _, want := range []string{
		"rw:" + job,
		"ro:/lib",
		"ro:/usr/lib",
		"ro:/etc/ld.so.cache",
		"ro:/proc/self/auxv",
		"ro:/proc/cpuinfo",
		"ro:/sys/devices/system/cpu/online",
		"ro:" + filepath.Join(dir, "vips"),
	} {
		if !strings.Contains(spec, want) {
			t.Errorf("missing %s in %s", want, spec)
		}
	}
	if strings.Contains(spec, "ro:/usr/share") || strings.Contains(spec, "ro:/etc/localtime") {
		t.Fatalf("spec %s", spec)
	}
	jobGrant := "rw:" + job
	if jobGrant == "rw:/" || jobGrant == "rw:/tmp" || jobGrant == "rw:/tmp/" {
		t.Fatalf("job directory is a shared path: %s", job)
	}
	for _, token := range strings.Split(spec, "\n") {
		if token == jobGrant {
			continue
		}
		if token == "rw:/" || token == "rw:/tmp" || strings.HasPrefix(token, "rw:/tmp/") || token == "ro:/tmp" || token == "ro:/" {
			t.Fatalf("spec grants a shared path: %s", spec)
		}
	}
	var bare LibvipsConverter
	if got := bare.jobExec(job).spec; len(got) != 0 {
		t.Fatalf("unconfined spec %q", got)
	}
}

func TestImageDeniedOutsideRejectsSetupFailureAndAWrittenFile(t *testing.T) {
	if err := imageDeniedOutside(ErrEngineSandboxUnavailable, ""); err == nil || !strings.Contains(err.Error(), "sandbox setup failed") {
		t.Fatal(err)
	}
	dir := t.TempDir()
	written := filepath.Join(dir, "neg.webp")
	if err := os.WriteFile(written, []byte("RIFF"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := imageDeniedOutside(errors.New("permission denied"), written); err == nil || !strings.Contains(err.Error(), "engine wrote") {
		t.Fatal(err)
	}
	if err := imageDeniedOutside(errors.New("permission denied"), filepath.Join(dir, "missing.webp")); err != nil {
		t.Fatal(err)
	}
	if err := imageDeniedOutside(nil, written); err == nil || !strings.Contains(err.Error(), "accepted") {
		t.Fatal(err)
	}
}
