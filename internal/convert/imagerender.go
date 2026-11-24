package convert

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strconv"
)

func init() {
	Default.Register(ImageRenderConverter{})
}

const (
	imageRenderDefault = 1280
	imageRenderMin     = 256
	imageRenderMax     = 2048
)

// ImageRenderConverter draws one raster file as a primary square PNG.
type ImageRenderConverter struct {
	run commandRunner
}

// Operation reports the render operation.
func (ImageRenderConverter) Operation() string { return OperationRender }

// Pairs returns png, jpg, webp, heic, and tiff, each to png.
func (ImageRenderConverter) Pairs() []Pair {
	pairs := make([]Pair, 0, len(imageFormats))
	for _, from := range imageFormats {
		pairs = append(pairs, Pair{From: from, To: "png"})
	}
	return pairs
}

// Convert writes one opaque square PNG at outPath.
// A nil or empty opts map uses edge 1280. The only accepted key is size.
func (c ImageRenderConverter) Convert(ctx context.Context, inPath, outPath string, opts map[string]any) error {
	fitPath := filepath.Join(filepath.Dir(outPath), "fit.png")
	if err := c.draw(ctx, inPath, outPath, fitPath, opts); err != nil {
		_ = os.Remove(outPath)
		_ = os.Remove(fitPath)
		return err
	}
	_ = os.Remove(fitPath)
	return nil
}

func (c ImageRenderConverter) draw(ctx context.Context, inPath, outPath, fitPath string, opts map[string]any) error {
	edge, err := imageRenderEdge(opts)
	if err != nil {
		return err
	}
	src := NormalizeFormat(filepath.Ext(inPath))
	tgt := NormalizeFormat(filepath.Ext(outPath))
	if !imageRenderPair(src, tgt) {
		return fmt.Errorf("unsupported conversion %s -> %s", src, tgt)
	}
	info, err := os.Stat(inPath)
	if err != nil {
		return fmt.Errorf("stat input: %w", err)
	}
	if info.Size() > MaxFileBytes {
		return fmt.Errorf("%w: %d bytes exceeds %d", ErrInputTooLarge, info.Size(), MaxFileBytes)
	}
	args := []string{
		"thumbnail", inPath, fitPath, strconv.Itoa(edge),
		"--height", strconv.Itoa(edge),
		"--size", "both",
	}
	if err := c.exec(ctx, filepath.Dir(outPath), args); err != nil {
		return err
	}
	f, err := os.Open(fitPath)
	if err != nil {
		return fmt.Errorf("image render: open fit: %w", err)
	}
	fitted, err := png.Decode(f)
	closeErr := f.Close()
	if err != nil {
		return fmt.Errorf("image render: decode fit: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("image render: close fit: %w", closeErr)
	}
	bounds := fitted.Bounds()
	dw, dh := bounds.Dx(), bounds.Dy()
	if dw < 1 || dh < 1 || dw > edge || dh > edge || max(dw, dh) != edge {
		return fmt.Errorf("image render: fitted size %dx%d", dw, dh)
	}
	dst := image.NewNRGBA(image.Rect(0, 0, edge, edge))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.NRGBA{R: 255, G: 255, B: 255, A: 255}), image.Point{}, draw.Src)
	ox := (edge - dw) / 2
	oy := (edge - dh) / 2
	draw.Draw(dst, image.Rect(ox, oy, ox+dw, oy+dh), fitted, bounds.Min, draw.Over)
	out, err := os.OpenFile(outPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("image render: create output: %w", err)
	}
	err = png.Encode(out, dst)
	closeErr = out.Close()
	if err != nil {
		return fmt.Errorf("image render: encode: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("image render: close output: %w", closeErr)
	}
	if err := validateImageOutput("png", outPath); err != nil {
		return fmt.Errorf("image render: %w", err)
	}
	return nil
}

func (c ImageRenderConverter) exec(ctx context.Context, workDir string, args []string) error {
	if c.run != nil {
		return c.run(ctx, "vips", args...)
	}
	live := Current()
	if live.sandbox.confines() {
		ex := live.jobExec(workDir)
		cmdName, cmdArgs, err := ex.sandbox.wrapEngine("vips", ex.spec, args)
		if err != nil {
			return err
		}
		if err := runCommandEnv(ctx, ex.env, cmdName, cmdArgs...); err != nil {
			return ex.sandbox.sandboxFault(err)
		}
		return nil
	}
	if err := runCommand(ctx, "vips", args...); err != nil {
		return fmt.Errorf("image render: %w", err)
	}
	return nil
}

func imageRenderPair(from, to string) bool {
	if to != "png" {
		return false
	}
	for _, src := range imageFormats {
		if src == from {
			return true
		}
	}
	return false
}

func imageRenderEdge(opts map[string]any) (int, error) {
	if len(opts) == 0 {
		return imageRenderDefault, nil
	}
	if len(opts) != 1 {
		return 0, fmt.Errorf("image render: unsupported option")
	}
	raw, ok := opts["size"]
	if !ok {
		return 0, fmt.Errorf("image render: unsupported option")
	}
	n, err := imageRenderSizeInt(raw)
	if err != nil {
		return 0, err
	}
	if n < imageRenderMin || n > imageRenderMax {
		return 0, fmt.Errorf("image render: size %d out of range", n)
	}
	return n, nil
}

func imageRenderSizeInt(raw any) (int, error) {
	switch v := raw.(type) {
	case int:
		return v, nil
	case int32:
		return int(v), nil
	case int64:
		return int(v), nil
	case float64:
		if v != math.Trunc(v) || math.IsInf(v, 0) || math.IsNaN(v) {
			return 0, fmt.Errorf("image render: size is not an integer")
		}
		return int(v), nil
	default:
		return 0, fmt.Errorf("image render: size is not an integer")
	}
}
