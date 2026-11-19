package convert

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPairsMatchManifest(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "schema", "manifest-image-convert.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Capabilities []struct {
			ID     string `json:"id"`
			Schema any    `json:"parameter_schema"`
			Legacy struct {
				Operation string `json:"operation"`
				Engine    string `json:"engine"`
			} `json:"legacy"`
			Formats []Pair `json:"formats"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Capabilities) != 1 {
		t.Fatalf("capabilities %d", len(doc.Capabilities))
	}
	capability := doc.Capabilities[0]
	if capability.ID != "octoconv.convert.image" || capability.Legacy.Engine != EngineImage || capability.Legacy.Operation != OperationConvert {
		t.Fatalf("%+v", capability)
	}
	schema, err := json.Marshal(capability.Schema)
	if err != nil {
		t.Fatal(err)
	}
	if string(schema) != "{}" {
		t.Fatalf("schema %s", schema)
	}
	got := (LibvipsConverter{}).Pairs()
	if len(got) != 20 || len(capability.Formats) != len(got) {
		t.Fatalf("pairs %d manifest %d", len(got), len(capability.Formats))
	}
	for i := range got {
		if got[i] != capability.Formats[i] {
			t.Fatalf("pair %d got %+v manifest %+v", i, got[i], capability.Formats[i])
		}
	}
	if _, ok := Default.LookupOp(OperationConvert, "jpeg", "tif"); !ok {
		t.Fatal("jpeg to tif")
	}
	if _, ok := Default.LookupOp(OperationConvert, "png", "png"); ok {
		t.Fatal("png to png")
	}
	if _, ok := Default.LookupOp(OperationConvert, "md", "html"); ok {
		t.Fatal("md to html")
	}
}

func TestMIMEAndTimeout(t *testing.T) {
	if MIMEType("jpg") != "image/jpeg" || MIMEType("JPEG") != "image/jpeg" || MIMEType("tif") != "image/tiff" || MIMEType("png") != "image/png" || MIMEType("webp") != "image/webp" || MIMEType("heic") != "image/heic" {
		t.Fatalf("mime jpg %s tif %s", MIMEType("jpg"), MIMEType("tif"))
	}
	if DefaultEngineTimeout.Seconds() != 120 || MaxFileBytes != 100<<20 {
		t.Fatalf("timeout %s ceiling %d", DefaultEngineTimeout, MaxFileBytes)
	}
}

func TestConvertRejectsParametersSizeAndPairsBeforeRun(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.png")
	out := filepath.Join(dir, "out.webp")
	if err := os.WriteFile(in, []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, 0o600); err != nil {
		t.Fatal(err)
	}
	var calls int
	c := &LibvipsConverter{run: func(context.Context, string, ...string) error {
		calls++
		return nil
	}}
	if err := c.Convert(context.Background(), in, out, map[string]any{"quality": 1}); err == nil || calls != 0 {
		t.Fatalf("parameters err %v calls %d", err, calls)
	}
	if err := c.Convert(context.Background(), in, filepath.Join(dir, "out.png"), nil); err == nil || calls != 0 {
		t.Fatalf("same pair err %v calls %d", err, calls)
	}
	if err := c.Convert(context.Background(), filepath.Join(dir, "in.md"), out, nil); err == nil || calls != 0 {
		t.Fatalf("markup err %v calls %d", err, calls)
	}
	c.maxInputBytes = 4
	big := filepath.Join(dir, "big.jpeg")
	if err := os.WriteFile(big, []byte{0xFF, 0xD8, 0xFF, 0x00, 0x00}, 0o600); err != nil {
		t.Fatal(err)
	}
	err := c.Convert(context.Background(), big, filepath.Join(dir, "big.tiff"), nil)
	if err == nil || !strings.Contains(err.Error(), ErrInputTooLarge.Error()) || calls != 0 {
		t.Fatalf("size err %v calls %d", err, calls)
	}
}

func TestConvertChecksTheOutputMagic(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.jpg")
	out := filepath.Join(dir, "out.webp")
	if err := os.WriteFile(in, []byte{0xFF, 0xD8, 0xFF}, 0o600); err != nil {
		t.Fatal(err)
	}
	webp := append([]byte("RIFF"), []byte{0, 0, 0, 0}...)
	webp = append(webp, []byte("WEBP")...)
	c := &LibvipsConverter{run: func(context.Context, string, ...string) error {
		return os.WriteFile(out, webp, 0o600)
	}}
	if err := c.Convert(context.Background(), in, out, nil); err != nil {
		t.Fatal(err)
	}
	c.run = func(context.Context, string, ...string) error {
		return os.WriteFile(out, []byte("not an image"), 0o600)
	}
	if err := c.Convert(context.Background(), in, out, nil); err == nil || !strings.Contains(err.Error(), "output is not webp") {
		t.Fatal(err)
	}
}

func TestImageMagic(t *testing.T) {
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	if !imageMagic("png", png) || imageMagic("jpg", png) {
		t.Fatal("png")
	}
	jpg := []byte{0xFF, 0xD8, 0xFF, 0xE0}
	if !imageMagic("jpeg", jpg) {
		t.Fatal("jpeg")
	}
	le := []byte{0x49, 0x49, 0x2A, 0x00}
	be := []byte{0x4D, 0x4D, 0x00, 0x2A}
	if !imageMagic("tif", le) || !imageMagic("tiff", be) {
		t.Fatal("tiff")
	}
	heic := make([]byte, 12)
	copy(heic[4:12], []byte("ftypheic"))
	if !imageMagic("heic", heic) {
		t.Fatal("heic")
	}
	copy(heic[8:12], []byte("mp42"))
	if imageMagic("heic", heic) {
		t.Fatal("mp4 brand")
	}
	var buf bytes.Buffer
	body, err := preflightPNG()
	if err != nil {
		t.Fatal(err)
	}
	buf.Write(body)
	if !imageMagic("png", buf.Bytes()) {
		t.Fatal("generated png")
	}
}
