package convert

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
)

// Measured read-only set for vips. The binary is added at run time: vips is
// execve'd and never opened, so a trace-only set fails with exec permission denied.
var imageReadOnly = []string{
	"/lib",
	"/usr/lib",
	"/etc/ld.so.cache",
	"/proc/self/auxv",
	"/proc/self/status",
	"/proc/filesystems",
	"/proc/cpuinfo",
	"/sys/devices/system/cpu/online",
	"/sys/devices/system/cpu/possible",
}

var imageEngines = []string{"vips"}

const (
	ImageSandboxDisabledEnv = "IMAGE_SANDBOX_DISABLED"
	imagePreflightOutside   = "/var/tmp"
)

func (c *LibvipsConverter) WithSandbox(helper []string) *LibvipsConverter {
	c.sandbox = newEngineSandbox(helper)
	return c
}

func Current() *LibvipsConverter { return live }

func (c *LibvipsConverter) jobExec(workDir string) engineExec {
	if resolved, err := filepath.EvalSymlinks(workDir); err == nil {
		workDir = resolved
	}
	return c.sandbox.forJobRW(workDir, nil, imageReadOnly, imageEngines).withEnv(imageEngineEnv(workDir))
}

// ImageProof is what ImagePreflight observed inside the job directory.
type ImageProof struct {
	OutputBytes int
}

func ImagePreflight(ctx context.Context, c *LibvipsConverter) (ImageProof, error) {
	if c == nil || !c.sandbox.confines() {
		return ImageProof{}, nil
	}
	w, cleanup, err := newPreflightWorkspaceOutside("image", imagePreflightOutside)
	if err != nil {
		return ImageProof{}, err
	}
	defer cleanup()
	grants := append(append([]string{}, imageReadOnly...), imageEnginePaths()...)
	if err := w.requireOutsideGrants(grants); err != nil {
		return ImageProof{}, err
	}
	pngBytes, err := preflightPNG()
	if err != nil {
		return ImageProof{}, err
	}
	inside, outside, err := w.place("in.png", pngBytes)
	if err != nil {
		return ImageProof{}, err
	}
	out := filepath.Join(w.jobDir, "out.webp")
	if err := c.Convert(ctx, inside, out, nil); err != nil {
		return ImageProof{}, fmt.Errorf("inside route: %w", err)
	}
	webp, err := os.ReadFile(out)
	if err != nil {
		return ImageProof{}, fmt.Errorf("inside route: %w", err)
	}
	if !imageMagic("webp", webp) {
		return ImageProof{}, fmt.Errorf("inside route: output is not webp")
	}
	negOut := filepath.Join(w.jobDir, "neg.webp")
	convErr := c.Convert(ctx, outside, negOut, nil)
	if err := imageDeniedOutside(convErr, negOut); err != nil {
		return ImageProof{}, fmt.Errorf("read a file outside the job directory: %w", err)
	}
	return ImageProof{OutputBytes: len(webp)}, nil
}

func imageDeniedOutside(convErr error, outPath string) error {
	if convErr == nil {
		return fmt.Errorf("engine accepted the outside file")
	}
	if errors.Is(convErr, ErrEngineSandboxUnavailable) {
		return fmt.Errorf("sandbox setup failed: %w", convErr)
	}
	info, err := os.Stat(outPath)
	if err != nil {
		return nil
	}
	if info.Size() > 0 {
		return fmt.Errorf("engine wrote the outside file")
	}
	return nil
}

func preflightPNG() ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		return nil, fmt.Errorf("preflight input: %w", err)
	}
	return buf.Bytes(), nil
}

func imageEnginePaths() []string {
	var paths []string
	for _, name := range imageEngines {
		if p, err := exec.LookPath(name); err == nil {
			paths = append(paths, p)
		}
	}
	return paths
}

func imageEngineEnv(tmpDir string) []string {
	keep := []string{"PATH", "HOME", "LANG", "LC_ALL"}
	env := make([]string, 0, len(keep)+1)
	for _, key := range keep {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	if tmpDir != "" {
		env = append(env, "TMPDIR="+tmpDir)
	}
	return env
}
