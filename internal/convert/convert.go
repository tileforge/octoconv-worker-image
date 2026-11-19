// Package convert turns one raster image into another with the vips CLI.
package convert

import (
	"context"
	"errors"
	"strings"
	"time"
)

// ErrOutputTooLarge means the result exceeds the output ceiling.
var ErrOutputTooLarge = errors.New("output too large")

// ErrInputTooLarge means the input exceeds the image ceiling.
var ErrInputTooLarge = errors.New("input too large")

const (
	EngineImage                = "image"
	OperationConvert           = "convert"
	DefaultEngineTimeout       = 120 * time.Second
	MaxFileBytes         int64 = 100 << 20
)

// Pair is one source format and one target format.
type Pair struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Converter converts the file at inPath into outPath.
type Converter interface {
	Pairs() []Pair
	Convert(ctx context.Context, inPath, outPath string, opts map[string]any) error
}

var formatAliases = map[string]string{
	"jpeg": "jpg",
	"tif":  "tiff",
}

// MIMEType is the content type the core stores for format.
func MIMEType(format string) string {
	switch NormalizeFormat(format) {
	case "png":
		return "image/png"
	case "jpg":
		return "image/jpeg"
	case "webp":
		return "image/webp"
	case "heic":
		return "image/heic"
	case "tiff":
		return "image/tiff"
	default:
		return "application/octet-stream"
	}
}

// NormalizeFormat lowercases a format or extension, strips one leading dot, and folds jpeg and tif.
func NormalizeFormat(f string) string {
	f = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(f), "."))
	if canonical, ok := formatAliases[f]; ok {
		return canonical
	}
	return f
}

type opKey struct {
	Operation string
	From      string
	To        string
}

// Registry maps an operation and a format pair to a converter.
type Registry struct {
	m map[opKey]Converter
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{m: make(map[opKey]Converter)}
}

// Register indexes each pair c reports.
func (r *Registry) Register(c Converter) {
	for _, pair := range c.Pairs() {
		r.m[opKey{
			Operation: OperationConvert,
			From:      NormalizeFormat(pair.From),
			To:        NormalizeFormat(pair.To),
		}] = c
	}
}

// LookupOp reports the converter for op, from, and to.
func (r *Registry) LookupOp(op, from, to string) (Converter, bool) {
	c, ok := r.m[opKey{
		Operation: op,
		From:      NormalizeFormat(from),
		To:        NormalizeFormat(to),
	}]
	return c, ok
}

// Default is the process registry.
var Default = NewRegistry()
