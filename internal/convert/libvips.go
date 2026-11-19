package convert

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// imageFormats are the raster formats vips converts between in this process.
var imageFormats = []string{"png", "jpg", "webp", "heic", "tiff"}

type commandRunner func(ctx context.Context, name string, args ...string) error

// LibvipsConverter runs vips copy for the closed pair table.
type LibvipsConverter struct {
	run           commandRunner
	sandbox       *engineSandbox
	maxInputBytes int64
}

var live = &LibvipsConverter{}

func init() {
	Default.Register(live)
}

// Pairs returns every ordered pair of imageFormats with from != to.
func (LibvipsConverter) Pairs() []Pair {
	pairs := make([]Pair, 0, len(imageFormats)*(len(imageFormats)-1))
	for _, from := range imageFormats {
		for _, to := range imageFormats {
			if from != to {
				pairs = append(pairs, Pair{From: from, To: to})
			}
		}
	}
	return pairs
}

func imagePairSupported(from, to string) bool {
	for _, pair := range (LibvipsConverter{}).Pairs() {
		if pair.From == from && pair.To == to {
			return true
		}
	}
	return false
}

func (c *LibvipsConverter) inputLimit() int64 {
	if c != nil && c.maxInputBytes > 0 {
		return c.maxInputBytes
	}
	return MaxFileBytes
}

// Convert runs `vips copy`. Any parameter key fails before vips starts.
func (c *LibvipsConverter) Convert(ctx context.Context, inPath, outPath string, opts map[string]any) error {
	if len(opts) > 0 {
		return errors.New("parameters")
	}
	src := NormalizeFormat(filepath.Ext(inPath))
	tgt := NormalizeFormat(filepath.Ext(outPath))
	if !imagePairSupported(src, tgt) {
		return fmt.Errorf("unsupported conversion %s -> %s", src, tgt)
	}
	info, err := os.Stat(inPath)
	if err != nil {
		return fmt.Errorf("stat input: %w", err)
	}
	limit := c.inputLimit()
	if info.Size() > limit {
		return fmt.Errorf("%w: %d bytes exceeds %d", ErrInputTooLarge, info.Size(), limit)
	}
	args := []string{"copy", inPath, outPath}
	if c.run != nil {
		if err := c.run(ctx, "vips", args...); err != nil {
			return err
		}
		return validateImageOutput(tgt, outPath)
	}
	if c.sandbox.confines() {
		ex := c.jobExec(filepath.Dir(outPath))
		cmdName, cmdArgs, err := ex.sandbox.wrapEngine("vips", ex.spec, args)
		if err != nil {
			return err
		}
		if err := runCommandEnv(ctx, ex.env, cmdName, cmdArgs...); err != nil {
			return ex.sandbox.sandboxFault(err)
		}
		return validateImageOutput(tgt, outPath)
	}
	if err := runCommand(ctx, "vips", args...); err != nil {
		return fmt.Errorf("libvips: %w", err)
	}
	return validateImageOutput(tgt, outPath)
}

func validateImageOutput(tgt, outPath string) error {
	f, _, err := OpenJobFile(outPath)
	if err != nil {
		return err
	}
	defer f.Close()
	buf := make([]byte, 12)
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return err
	}
	if !imageMagic(tgt, buf[:n]) {
		return fmt.Errorf("output is not %s", tgt)
	}
	return nil
}

var heicBrands = map[string]bool{
	"heic": true,
	"heix": true,
	"hevc": true,
	"hevx": true,
	"mif1": true,
	"msf1": true,
}

func imageMagic(format string, b []byte) bool {
	switch NormalizeFormat(format) {
	case "png":
		sig := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
		return len(b) >= len(sig) && bytes.Equal(b[:len(sig)], sig)
	case "jpg":
		sig := []byte{0xFF, 0xD8, 0xFF}
		return len(b) >= len(sig) && bytes.Equal(b[:len(sig)], sig)
	case "webp":
		return len(b) >= 12 && bytes.Equal(b[0:4], []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WEBP"))
	case "tiff":
		le := []byte{0x49, 0x49, 0x2A, 0x00}
		be := []byte{0x4D, 0x4D, 0x00, 0x2A}
		return len(b) >= 4 && (bytes.Equal(b[:4], le) || bytes.Equal(b[:4], be))
	case "heic":
		if len(b) < 12 || !bytes.Equal(b[4:8], []byte("ftyp")) {
			return false
		}
		return heicBrands[string(b[8:12])]
	default:
		return false
	}
}
