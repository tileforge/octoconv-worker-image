package convert

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestImageRenderManifest(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "schema", "manifest-image-render.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Capabilities []struct {
			ID              string `json:"id"`
			Version         int    `json:"version"`
			ClientOperation string `json:"client_operation"`
			Schema          any    `json:"parameter_schema"`
			Legacy          struct {
				Operation string `json:"operation"`
				Engine    string `json:"engine"`
			} `json:"legacy"`
			Inputs []struct {
				Role string `json:"role"`
				Min  int    `json:"min"`
				Max  int    `json:"max"`
			} `json:"inputs"`
			Outputs []struct {
				Role string `json:"role"`
				Min  int    `json:"min"`
				Max  int    `json:"max"`
			} `json:"outputs"`
			Formats []Pair `json:"formats"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Capabilities) != 1 {
		t.Fatalf("capabilities %d", len(doc.Capabilities))
	}
	cap := doc.Capabilities[0]
	if cap.ID != "octoconv.render.image" || cap.Version != 1 || cap.Legacy.Operation != OperationRender || cap.Legacy.Engine != EngineImage || cap.ClientOperation != OperationRender {
		t.Fatalf("%+v", cap)
	}
	if len(cap.Inputs) != 1 || cap.Inputs[0].Role != "source" || cap.Inputs[0].Min != 1 || cap.Inputs[0].Max != 1 {
		t.Fatalf("inputs %+v", cap.Inputs)
	}
	if len(cap.Outputs) != 1 || cap.Outputs[0].Role != "primary" || cap.Outputs[0].Min != 1 || cap.Outputs[0].Max != 1 {
		t.Fatalf("outputs %+v", cap.Outputs)
	}
	schema, err := json.Marshal(cap.Schema)
	if err != nil {
		t.Fatal(err)
	}
	if string(schema) != "{}" {
		t.Fatalf("schema %s", schema)
	}
	got := (ImageRenderConverter{}).Pairs()
	if len(got) != len(cap.Formats) {
		t.Fatalf("pairs %d manifest %d", len(got), len(cap.Formats))
	}
	for i := range got {
		if got[i] != cap.Formats[i] {
			t.Fatalf("pair %d got %+v manifest %+v", i, got[i], cap.Formats[i])
		}
	}
	want := []Pair{{"png", "png"}, {"jpg", "png"}, {"webp", "png"}, {"heic", "png"}, {"tiff", "png"}}
	if !slices.Equal(got, want) {
		t.Fatalf("%+v", got)
	}
	if _, ok := Default.LookupOp(OperationRender, "png", "png"); !ok {
		t.Fatal("png to png")
	}
	if _, ok := Default.LookupOp(OperationRender, "jpeg", "png"); !ok {
		t.Fatal("jpeg to png")
	}
	if _, ok := Default.LookupOp(OperationRender, "tif", "png"); !ok {
		t.Fatal("tif to png")
	}
	if _, ok := Default.LookupOp(OperationConvert, "png", "png"); ok {
		t.Fatal("convert png to png")
	}
	if MIMEType("png") != "image/png" {
		t.Fatal(MIMEType("png"))
	}
}

func TestImageRenderLetterbox(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.png")
	out := filepath.Join(dir, "out.png")
	if err := os.WriteFile(in, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	var calls int
	var gotName string
	var gotArgs []string
	c := ImageRenderConverter{run: func(_ context.Context, name string, args ...string) error {
		calls++
		gotName = name
		gotArgs = append([]string(nil), args...)
		return writeSolidPNG(args[2], 128, 256, color.NRGBA{R: 255, A: 255})
	}}
	if err := c.Convert(context.Background(), in, out, map[string]any{"size": 256}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || gotName != "vips" {
		t.Fatalf("calls %d name %s", calls, gotName)
	}
	if len(gotArgs) != 8 || gotArgs[0] != "thumbnail" || gotArgs[1] != in || gotArgs[3] != "256" || gotArgs[4] != "--height" || gotArgs[5] != "256" || gotArgs[6] != "--size" || gotArgs[7] != "both" {
		t.Fatalf("%q", gotArgs)
	}
	fit := gotArgs[2]
	if filepath.Dir(fit) != dir || fit == out || fit == in {
		t.Fatalf("fit %s", fit)
	}
	for _, arg := range gotArgs {
		for _, bad := range []string{"--no-rotate", "--crop", "force", "nearest", "resize", "autorot", "page"} {
			if strings.Contains(arg, bad) {
				t.Fatalf("arg %s contains %s", arg, bad)
			}
		}
	}
	img := decodePNG(t, out)
	if img.Bounds().Dx() != 256 || img.Bounds().Dy() != 256 {
		t.Fatalf("%v", img.Bounds())
	}
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			px := nrgbaAt(img, x, y)
			if px.A != 255 {
				t.Fatalf("%d,%d alpha %d", x, y, px.A)
			}
			inside := x >= 64 && x < 192
			if inside {
				if px.R != 255 || px.G != 0 || px.B != 0 {
					t.Fatalf("%d,%d %+v", x, y, px)
				}
			} else if px != (color.NRGBA{R: 255, G: 255, B: 255, A: 255}) {
				t.Fatalf("%d,%d %+v", x, y, px)
			}
		}
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte("pHYs")) {
		t.Fatal("pHYs")
	}
	if _, err := os.Stat(fit); !os.IsNotExist(err) {
		t.Fatal("fit remains")
	}
}

func TestImageRenderUnserved(t *testing.T) {
	if _, ok := Default.LookupOp(OperationRender, "gif", "png"); ok {
		t.Fatal("gif")
	}
	if _, ok := Default.LookupOp(OperationRender, "pdf", "png"); ok {
		t.Fatal("pdf")
	}
	if _, ok := Default.LookupOp(OperationRender, "png", "jpg"); ok {
		t.Fatal("png to jpg")
	}
	dir := t.TempDir()
	var calls int
	c := ImageRenderConverter{run: func(context.Context, string, ...string) error {
		calls++
		return nil
	}}
	pairs := [][2]string{{"in.gif", "out.png"}, {"in.pdf", "out.png"}, {"in.png", "out.jpg"}}
	for _, pair := range pairs {
		calls = 0
		in := filepath.Join(dir, pair[0])
		out := filepath.Join(dir, pair[1])
		if err := os.WriteFile(out, []byte("pre"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := c.Convert(context.Background(), in, out, nil); err == nil || calls != 0 {
			t.Fatalf("%s calls %d err %v", pair[0], calls, err)
		}
		if _, err := os.Stat(out); !os.IsNotExist(err) {
			t.Fatalf("%s remains", out)
		}
	}
}

func TestImageRenderLive(t *testing.T) {
	if _, err := exec.LookPath("vips"); err != nil {
		t.Skip("vips is not on PATH")
	}
	dir := t.TempDir()
	in := filepath.Join(dir, "in.png")
	out := filepath.Join(dir, "out.png")
	if err := writeSolidPNG(in, 2, 4, color.NRGBA{R: 255, A: 255}); err != nil {
		t.Fatal(err)
	}
	if err := (ImageRenderConverter{}).Convert(context.Background(), in, out, map[string]any{"size": 256}); err != nil {
		t.Fatal(err)
	}
	img := decodePNG(t, out)
	if img.Bounds().Dx() != 256 || img.Bounds().Dy() != 256 {
		t.Fatalf("%v", img.Bounds())
	}
	red := nrgbaAt(img, 128, 0)
	if red.R != 255 || red.G != 0 || red.B != 0 || red.A != 255 {
		t.Fatalf("top centre %+v", red)
	}
	for _, pt := range [][2]int{{0, 0}, {0, 128}} {
		px := nrgbaAt(img, pt[0], pt[1])
		if px != (color.NRGBA{R: 255, G: 255, B: 255, A: 255}) {
			t.Fatalf("%d,%d %+v", pt[0], pt[1], px)
		}
	}
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			if nrgbaAt(img, x, y).A != 255 {
				t.Fatalf("%d,%d alpha", x, y)
			}
		}
	}
}

func TestImageRenderSize(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.png")
	if err := os.WriteFile(in, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	accept := []struct {
		name string
		opts map[string]any
		edge string
	}{
		{name: "empty", opts: map[string]any{}, edge: "1280"},
		{name: "int 256", opts: map[string]any{"size": 256}, edge: "256"},
		{name: "int32 256", opts: map[string]any{"size": int32(256)}, edge: "256"},
		{name: "int64 256", opts: map[string]any{"size": int64(256)}, edge: "256"},
		{name: "float 256", opts: map[string]any{"size": float64(256)}, edge: "256"},
		{name: "int 2048", opts: map[string]any{"size": 2048}, edge: "2048"},
		{name: "int32 2048", opts: map[string]any{"size": int32(2048)}, edge: "2048"},
		{name: "int64 2048", opts: map[string]any{"size": int64(2048)}, edge: "2048"},
		{name: "float 2048", opts: map[string]any{"size": float64(2048)}, edge: "2048"},
	}
	for _, tc := range accept {
		var calls int
		var width string
		c := ImageRenderConverter{run: func(_ context.Context, name string, args ...string) error {
			calls++
			if name != "vips" || len(args) < 6 {
				t.Fatalf("%s argv %s %v", tc.name, name, args)
			}
			width = args[3]
			if args[5] != args[3] {
				t.Fatalf("%s height %s", tc.name, args[5])
			}
			return errors.New("stop")
		}}
		out := filepath.Join(dir, "out-"+tc.edge+".png")
		if err := os.WriteFile(out, []byte("pre"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := c.Convert(context.Background(), in, out, tc.opts); err == nil || calls != 1 || width != tc.edge {
			t.Fatalf("%s err %v calls %d width %s", tc.name, err, calls, width)
		}
		if _, err := os.Stat(out); !os.IsNotExist(err) {
			t.Fatalf("%s remains", tc.name)
		}
	}
	var calls int
	var width, height string
	c := ImageRenderConverter{run: func(_ context.Context, _ string, args ...string) error {
		calls++
		if len(args) >= 6 {
			width = args[3]
			height = args[5]
		}
		return errors.New("stop")
	}}
	out := filepath.Join(dir, "nil.png")
	if err := os.WriteFile(out, []byte("pre"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := c.Convert(context.Background(), in, out, nil); err == nil || calls != 1 || width != "1280" || height != "1280" {
		t.Fatalf("nil err %v calls %d width %s height %s", err, calls, width, height)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("nil out remains")
	}
	reject := []map[string]any{
		{"size": 255},
		{"size": 2049},
		{"size": 1.5},
		{"size": math.NaN()},
		{"size": math.Inf(1)},
		{"size": "256"},
		{"size": true},
		{"size": 256, "quality": 1},
	}
	for _, opts := range reject {
		calls = 0
		out := filepath.Join(dir, "bad.png")
		if err := os.WriteFile(out, []byte("pre"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := c.Convert(context.Background(), in, out, opts); err == nil || calls != 0 {
			t.Fatalf("%#v err %v calls %d", opts, err, calls)
		}
		if _, err := os.Stat(out); !os.IsNotExist(err) {
			t.Fatalf("%#v remains", opts)
		}
	}
}

func TestImageRenderOpaque(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.png")
	out := filepath.Join(dir, "out.png")
	if err := os.WriteFile(in, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	var calls int
	c := ImageRenderConverter{run: func(_ context.Context, _ string, args ...string) error {
		calls++
		img := image.NewNRGBA(image.Rect(0, 0, 256, 256))
		red := color.NRGBA{R: 255, A: 255}
		for y := 0; y < 256; y++ {
			for x := 0; x < 256; x++ {
				img.SetNRGBA(x, y, red)
			}
		}
		img.SetNRGBA(1, 0, color.NRGBA{R: 255, A: 0})
		img.SetNRGBA(0, 1, color.NRGBA{R: 255, A: 128})
		return writeNRGBA(args[2], img)
	}}
	if err := c.Convert(context.Background(), in, out, map[string]any{"size": 256}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal(calls)
	}
	img := decodePNG(t, out)
	if img.Bounds().Dx() != 256 || img.Bounds().Dy() != 256 {
		t.Fatalf("%v", img.Bounds())
	}
	origin := nrgbaAt(img, 0, 0)
	if origin != (color.NRGBA{R: 255, A: 255}) {
		t.Fatalf("origin %+v", origin)
	}
	clear := nrgbaAt(img, 1, 0)
	if clear != (color.NRGBA{R: 255, G: 255, B: 255, A: 255}) {
		t.Fatalf("clear %+v", clear)
	}
	half := nrgbaAt(img, 0, 1)
	pure := color.NRGBA{R: 255, A: 255}
	white := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	if half.A != 255 || half == pure || half == white {
		t.Fatalf("half %+v", half)
	}
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			if nrgbaAt(img, x, y).A != 255 {
				t.Fatalf("%d,%d alpha", x, y)
			}
		}
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte("pHYs")) {
		t.Fatal("pHYs")
	}
}

func TestImageRenderEngineDown(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	dir := t.TempDir()
	in := filepath.Join(dir, "in.png")
	out := filepath.Join(dir, "out.png")
	if err := writeSolidPNG(in, 2, 2, color.NRGBA{R: 255, A: 255}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, []byte("pre"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := (ImageRenderConverter{}).Convert(context.Background(), in, out, nil); err == nil {
		t.Fatal("expected error")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "in.png" {
		t.Fatalf("dir %v", entries)
	}
}

func TestImageRenderFitDoesNotClobber(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.png")
	sibling := filepath.Join(dir, "fit.png")
	if err := os.WriteFile(in, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sibling, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	var calls int
	c := ImageRenderConverter{run: func(context.Context, string, ...string) error {
		calls++
		return nil
	}}
	out := filepath.Join(dir, "out.png")
	if err := os.WriteFile(out, []byte("pre"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := c.Convert(context.Background(), in, out, map[string]any{"size": 1}); err == nil || calls != 0 {
		t.Fatalf("err %v calls %d", err, calls)
	}
	body, err := os.ReadFile(sibling)
	if err != nil || string(body) != "keep" {
		t.Fatalf("sibling %q %v", body, err)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("out remains")
	}

	other := t.TempDir()
	in2 := filepath.Join(other, "in.png")
	named := filepath.Join(other, "fit.png")
	if err := os.WriteFile(in2, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	c.run = func(_ context.Context, _ string, args ...string) error {
		calls++
		if args[2] == named || args[2] == in2 {
			t.Fatalf("fit %s", args[2])
		}
		return writeSolidPNG(args[2], 256, 256, color.NRGBA{R: 255, A: 255})
	}
	if err := c.Convert(context.Background(), in2, named, map[string]any{"size": 256}); err != nil {
		t.Fatal(err)
	}
	img := decodePNG(t, named)
	if img.Bounds().Dx() != 256 || img.Bounds().Dy() != 256 {
		t.Fatalf("%v", img.Bounds())
	}
	entries, err := os.ReadDir(other)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"fit.png", "in.png"}) {
		t.Fatalf("%v", names)
	}
}

func writeSolidPNG(path string, w, h int, c color.NRGBA) error {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	return writeNRGBA(path, img)
}

func writeNRGBA(path string, img *image.NRGBA) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	err = png.Encode(f, img)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func decodePNG(t *testing.T, path string) image.Image {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func nrgbaAt(img image.Image, x, y int) color.NRGBA {
	return color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
}
