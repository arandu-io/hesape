package view_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/arandu-io/hesape/image"
	"github.com/arandu-io/hesape/view"
)

// This driver isolates registration from codecs. Real raster decoding and
// alpha preservation are exercised by consumers with the optional driver.
type assetImageDriver struct {
	image.Driver
	output []byte
	err    error
	calls  int
}

func (d *assetImageDriver) Process(_ context.Context, body []byte, p *image.ImagePipeline) ([]byte, error) {
	d.calls++
	if p.Output.Format != "webp" {
		return nil, errors.New("expected WebP pipeline")
	}
	body[0] = '!'
	return d.output, d.err
}

func assetWebP() []byte {
	return []byte("RIFF\x12\x00\x00\x00WEBPVP8L\x05\x00\x00\x00\x2f\x00\x00\x00\x00\x00")
}

func TestImageAssetResolvesItsSourceNameToTheWebPBytes(t *testing.T) {
	d := &assetImageDriver{output: assetWebP()}
	source := []byte("<svg/>")
	name := t.Name() + ".svg"
	wantBody := bytes.Clone(d.output)
	if err := view.RegisterImageAsset(context.Background(), name, source, d); err != nil {
		t.Fatal(err)
	}
	if string(source) != "<svg/>" {
		t.Fatal("conversion modified the caller's source")
	}
	d.output[0] = '!'
	target := t.Name() + ".webp"
	url := view.AssetPath + view.AssetHash(wantBody) + "/" + target
	if got := view.Asset(name); got != url || got != view.Asset(target) {
		t.Fatalf("source resolved to %s, want %s", got, url)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			rec := httptest.NewRecorder()
			view.Handler(rec, httptest.NewRequest(http.MethodGet, url, nil))
			if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/webp" || !bytes.Equal(rec.Body.Bytes(), wantBody) {
				t.Error("served image does not match its WebP registration")
			}
			if !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
				t.Error("image is not immutable")
			}
		})
	}
	wg.Wait()
	if d.calls != 1 {
		t.Fatalf("conversion ran %d times, want once before requests", d.calls)
	}
	found := 0
	for _, a := range view.Assets() {
		if a.Name == name {
			t.Fatal("Assets lists a source alias as a served SVG")
		}
		if a.Name == target && a.ContentType == "image/webp" && a.URL == url {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("found %d canonical registrations, want one", found)
	}
	rec := httptest.NewRecorder()
	view.Handler(rec, httptest.NewRequest(http.MethodGet, view.AssetPath+view.AssetHash(wantBody)+"/"+name, nil))
	if rec.Code != http.StatusNotFound {
		t.Fatal("WebP was served under an SVG name")
	}
}

func TestImageAssetConversionFailureDoesNotRegisterAFallback(t *testing.T) {
	for _, test := range []struct {
		name   string
		driver *assetImageDriver
	}{
		{"codec", &assetImageDriver{err: errors.New("unsupported SVG filter")}},
		{"wrong-format", &assetImageDriver{output: []byte("not WebP")}},
		{"truncated", &assetImageDriver{output: assetWebP()[:24]}},
	} {
		t.Run(test.name, func(t *testing.T) {
			name := strings.ReplaceAll(t.Name(), "/", "-") + ".svg"
			before := len(view.Assets())
			if err := view.RegisterImageAsset(context.Background(), name, []byte("<svg/>"), test.driver); err == nil {
				t.Fatal("invalid conversion succeeded")
			}
			if len(view.Assets()) != before {
				t.Fatal("failed conversion changed the registry")
			}
			view.RegisterAsset(name, "image/svg+xml", []byte("<svg/>"))
		})
	}
}

func TestImageAssetNamesCannotOverwriteOtherRegistrations(t *testing.T) {
	name := t.Name() + ".svg"
	d := &assetImageDriver{output: assetWebP()}
	if err := view.RegisterImageAsset(context.Background(), name, []byte("<svg/>"), d); err != nil {
		t.Fatal(err)
	}
	url := view.Asset(name)
	for _, conflicting := range []string{name, t.Name() + ".SVG"} {
		if err := view.RegisterImageAsset(context.Background(), conflicting, []byte("<svg/>"), d); err == nil {
			t.Fatalf("collision accepted: %s", conflicting)
		}
	}
	if view.Asset(name) != url {
		t.Fatal("collision changed the existing image")
	}
	defer func() {
		if recover() == nil {
			t.Error("RegisterAsset shadowed an image alias")
		}
	}()
	view.RegisterAsset(name, "image/svg+xml", []byte("<svg/>"))
}

func TestImageAssetRejectsInvalidNamesAndCancelledWork(t *testing.T) {
	d := &assetImageDriver{output: assetWebP()}
	for _, name := range []string{"", "../logo.svg", "logo.svg?x", "logo.svg#x", `a\logo.svg`, "/logo.svg", "logo.js", "logo.png", "logo.jpg", "logo.jpeg", "logo.gif", "logo.webp", "app.css"} {
		if err := view.RegisterImageAsset(context.Background(), name, []byte("<svg/>"), d); err == nil {
			t.Errorf("invalid name accepted: %s", name)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := view.RegisterImageAsset(ctx, "cancelled.svg", []byte("<svg/>"), d); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	if err := view.RegisterImageAsset(context.Background(), "empty.svg", nil, d); err == nil {
		t.Error("empty image accepted")
	}
	if err := view.RegisterImageAsset(context.Background(), "nil.svg", []byte("<svg/>"), nil); err == nil {
		t.Error("nil driver accepted")
	}
	if d.calls != 0 {
		t.Fatal("invalid input reached the codec")
	}
}
