package image_test

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	himage "github.com/arandu-io/hesape/image"
	"github.com/arandu-io/hesape/image/transformations"
)

const tenant = "acme"

func grant() auth.Grant { return auth.SystemGrant("image.write", tenant) }

// solidPNG is a w by h picture of one colour, encoded as PNG.
func solidPNG(t *testing.T, w, h int, c color.RGBA) []byte {
	t.Helper()
	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			canvas.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, canvas); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

// stripedPNG alternates black and white columns, one pixel wide.
func stripedPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := uint8(0)
			if x%2 == 1 {
				v = 255
			}
			canvas.SetRGBA(x, y, color.RGBA{R: v, G: v, B: v, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, canvas); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

// jpegWithOrientation is a w by h JPEG carrying an EXIF orientation tag, built
// by hand: the encoder in the standard library writes no metadata, so the APP1
// segment is spliced in behind the start-of-image marker exactly where a camera
// puts it.
func jpegWithOrientation(t *testing.T, orientation byte, w, h int) []byte {
	t.Helper()
	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			canvas.SetRGBA(x, y, color.RGBA{R: 10, G: 120, B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, canvas, nil); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	raw := buf.Bytes()

	body := []byte("Exif\x00\x00")
	body = append(body, 'I', 'I', 42, 0, 8, 0, 0, 0) // little endian TIFF, IFD at 8
	body = append(body, 1, 0)                        // one entry
	body = append(body, 0x12, 0x01, 3, 0, 1, 0, 0, 0, orientation, 0, 0, 0)
	body = append(body, 0, 0, 0, 0) // no next IFD

	length := len(body) + 2
	segment := []byte{0xFF, 0xE1, byte(length >> 8), byte(length)}
	segment = append(segment, body...)

	out := append([]byte{0xFF, 0xD8}, segment...)
	return append(out, raw[2:]...)
}

func decode(t *testing.T, b []byte) *image.RGBA {
	t.Helper()
	src, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("decoding the result: %v", err)
	}
	bounds := src.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			r, g, bl, a := src.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			out.SetRGBA(x, y, color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(bl >> 8), A: uint8(a >> 8)})
		}
	}
	return out
}

func size(t *testing.T, img *himage.Image) (int, int) {
	t.Helper()
	w, h, err := img.Dimensions()
	if err != nil {
		t.Fatalf("Dimensions: %v", err)
	}
	return w, h
}

// fakeDisk stands in for filesystem.Disk, and records what it was handed so a
// test can say what the Grant did to the call.
type fakeDisk struct {
	grant       auth.Grant
	key         string
	body        []byte
	contentType string
	visibility  string
	stored      map[string][]byte
}

func newFakeDisk() *fakeDisk { return &fakeDisk{stored: map[string][]byte{}} }

func (d *fakeDisk) Put(_ context.Context, g auth.Grant, key string, body io.Reader, contentType string) error {
	b, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	d.grant, d.key, d.body, d.contentType = g, key, b, contentType
	d.stored[key] = b
	return nil
}

func (d *fakeDisk) SetVisibility(_ context.Context, _ auth.Grant, key, visibility string) error {
	d.key, d.visibility = key, visibility
	return nil
}

func (d *fakeDisk) Get(_ context.Context, _ auth.Grant, key string) (io.ReadCloser, error) {
	b, ok := d.stored[key]
	if !ok {
		return nil, errors.New("no such key")
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

// TestFluentCallsLeaveTheOriginalAlone verifies the clone-on-write behaviour
// that lets one source image produce a thumbnail and a full-size copy.
func TestFluentCallsLeaveTheOriginalAlone(t *testing.T) {
	images := himage.NewImageManager()
	original := images.FromBytes(solidPNG(t, 40, 20, color.RGBA{R: 200, A: 255}))

	thumb := original.Cover(10, 10)

	if w, h := size(t, thumb); w != 10 || h != 10 {
		t.Fatalf("thumbnail = %dx%d, want 10x10", w, h)
	}
	if w, h := size(t, original); w != 40 || h != 20 {
		t.Fatalf("original = %dx%d, want 40x20: a fluent call mutated the receiver", w, h)
	}
}

// TestACloneDoesNotInheritTheAnswersOfTheImageItCameFrom: the width was read
// before the thumbnail was asked for, and the thumbnail must not report it.
func TestACloneDoesNotInheritTheAnswersOfTheImageItCameFrom(t *testing.T) {
	images := himage.NewImageManager()
	original := images.FromBytes(solidPNG(t, 40, 20, color.RGBA{A: 255}))

	if w, _ := size(t, original); w != 40 {
		t.Fatalf("width = %d, want 40", w)
	}
	if w, h := size(t, original.Cover(10, 10)); w != 10 || h != 10 {
		t.Fatalf("thumbnail = %dx%d, want 10x10: the clone answered for its source", w, h)
	}

	mediaType, err := original.MimeType()
	if err != nil {
		t.Fatalf("MimeType: %v", err)
	}
	if mediaType != "image/png" {
		t.Fatalf("mime = %s, want image/png", mediaType)
	}
	converted, err := original.ToJpg().MimeType()
	if err != nil {
		t.Fatalf("MimeType: %v", err)
	}
	if converted != "image/jpeg" {
		t.Fatalf("mime = %s, want image/jpeg: the clone answered for its source", converted)
	}
}

// TestNothingIsDecodedWhenNothingWasAsked: an image that needed no work leaves
// as the bytes that arrived, which is why storing an untouched upload costs
// nothing.
func TestNothingIsDecodedWhenNothingWasAsked(t *testing.T) {
	source := solidPNG(t, 4, 4, color.RGBA{G: 255, A: 255})
	out, err := himage.NewImageManager().FromBytes(source).ToBytes()
	if err != nil {
		t.Fatalf("ToBytes: %v", err)
	}
	if !bytes.Equal(out, source) {
		t.Fatal("the bytes were re-encoded although no transformation was asked for")
	}
}

func TestCoverFillsTheBoxExactly(t *testing.T) {
	images := himage.NewImageManager()
	img := images.FromBytes(solidPNG(t, 60, 20, color.RGBA{B: 255, A: 255})).Cover(10, 10)

	if w, h := size(t, img); w != 10 || h != 10 {
		t.Fatalf("cover = %dx%d, want 10x10", w, h)
	}
}

func TestContainFitsTheWholeImageAndPadsTheRest(t *testing.T) {
	images := himage.NewImageManager()
	// A wide image in a square box leaves bands above and below.
	img := images.FromBytes(solidPNG(t, 40, 20, color.RGBA{R: 255, A: 255})).
		Contain(20, 20, "#00ff00")

	out, err := img.ToBytes()
	if err != nil {
		t.Fatalf("ToBytes: %v", err)
	}
	canvas := decode(t, out)
	if w, h := canvas.Rect.Dx(), canvas.Rect.Dy(); w != 20 || h != 20 {
		t.Fatalf("contain = %dx%d, want 20x20", w, h)
	}
	if got := canvas.RGBAAt(0, 0); got.G < 250 || got.R > 5 {
		t.Fatalf("the padding is %v, want the green that was asked for", got)
	}
	if got := canvas.RGBAAt(10, 10); got.R < 250 || got.G > 5 {
		t.Fatalf("the middle is %v, want the red of the image", got)
	}
}

func TestContainUsesTheDominantColourWhenAsked(t *testing.T) {
	images := himage.NewImageManager()
	img := images.FromBytes(solidPNG(t, 40, 20, color.RGBA{R: 12, G: 34, B: 56, A: 255})).
		Contain(20, 20, "dominant")

	out, err := img.ToBytes()
	if err != nil {
		t.Fatalf("ToBytes: %v", err)
	}
	canvas := decode(t, out)
	got := canvas.RGBAAt(0, 0)
	if got.R != 12 || got.G != 34 || got.B != 56 {
		t.Fatalf("the padding is %v, want the image's own colour", got)
	}
}

func TestResizeWithNoDimensionIsRefused(t *testing.T) {
	images := himage.NewImageManager()
	if _, err := images.FromBytes(solidPNG(t, 4, 4, color.RGBA{A: 255})).Resize(0, 0); !errors.Is(err, himage.ErrImage) {
		t.Fatalf("err = %v, want ErrImage", err)
	}
	if _, err := images.FromBytes(solidPNG(t, 4, 4, color.RGBA{A: 255})).Scale(0, 0); !errors.Is(err, himage.ErrImage) {
		t.Fatalf("err = %v, want ErrImage", err)
	}
}

func TestResizeLeavesTheAxisThatWasNotGiven(t *testing.T) {
	images := himage.NewImageManager()
	img, err := images.FromBytes(solidPNG(t, 40, 20, color.RGBA{A: 255})).Resize(10, 0)
	if err != nil {
		t.Fatalf("Resize: %v", err)
	}
	if w, h := size(t, img); w != 10 || h != 20 {
		t.Fatalf("resize = %dx%d, want 10x20", w, h)
	}
}

func TestScaleNeverEnlarges(t *testing.T) {
	images := himage.NewImageManager()
	img, err := images.FromBytes(solidPNG(t, 40, 20, color.RGBA{A: 255})).Scale(400, 400)
	if err != nil {
		t.Fatalf("Scale: %v", err)
	}
	if w, h := size(t, img); w != 40 || h != 20 {
		t.Fatalf("scale = %dx%d, want the original 40x20: Scale is scaleDown", w, h)
	}
}

func TestScaleKeepsTheAspectRatio(t *testing.T) {
	images := himage.NewImageManager()
	img, err := images.FromBytes(solidPNG(t, 40, 20, color.RGBA{A: 255})).Scale(10, 10)
	if err != nil {
		t.Fatalf("Scale: %v", err)
	}
	if w, h := size(t, img); w != 10 || h != 5 {
		t.Fatalf("scale = %dx%d, want 10x5", w, h)
	}
}

// TestDownscaleAveragesInsteadOfDroppingPixels is the quality claim, stated as
// a test. Eight alternating black and white columns reduced to two pixels must
// come out grey: an implementation that samples two source pixels per output
// pixel -- plain bilinear -- or one that takes the nearest -- what comes free --
// produces black or white, having never looked at three quarters of the image.
func TestDownscaleAveragesInsteadOfDroppingPixels(t *testing.T) {
	images := himage.NewImageManager()
	img, err := images.FromBytes(stripedPNG(t, 8, 8)).Resize(2, 2)
	if err != nil {
		t.Fatalf("Resize: %v", err)
	}
	out, err := img.ToBytes()
	if err != nil {
		t.Fatalf("ToBytes: %v", err)
	}
	canvas := decode(t, out)
	for x := 0; x < 2; x++ {
		got := canvas.RGBAAt(x, 0).R
		if got < 100 || got > 155 {
			t.Fatalf("pixel %d is %d, want something near 128: the stripes were dropped rather than averaged", x, got)
		}
	}
}

// TestOrientAppliesTheExifTag: the photograph that arrives sideways.
func TestOrientAppliesTheExifTag(t *testing.T) {
	images := himage.NewImageManager()
	// Orientation 6 is "rotate 90 clockwise to display", which every phone
	// writes when it is held upright.
	img := images.FromBytes(jpegWithOrientation(t, 6, 40, 20)).Orient()

	if w, h := size(t, img); w != 20 || h != 40 {
		t.Fatalf("oriented = %dx%d, want 20x40: the EXIF tag was ignored", w, h)
	}
}

func TestOrientLeavesAnImageWithNoExifAlone(t *testing.T) {
	images := himage.NewImageManager()
	img := images.FromBytes(solidPNG(t, 40, 20, color.RGBA{A: 255})).Orient()

	if w, h := size(t, img); w != 40 || h != 20 {
		t.Fatalf("oriented = %dx%d, want 40x20", w, h)
	}
}

func TestOrientReadsTheTagBeforeTheImageIsReencoded(t *testing.T) {
	images := himage.NewImageManager()
	// Resize first: the EXIF block does not survive the re-encode, so the
	// orientation has to have been read from the bytes that arrived.
	img, err := images.FromBytes(jpegWithOrientation(t, 8, 40, 20)).Resize(20, 10)
	if err != nil {
		t.Fatalf("Resize: %v", err)
	}
	if w, h := size(t, img.Orient()); w != 10 || h != 20 {
		t.Fatalf("oriented = %dx%d, want 10x20", w, h)
	}
}

func TestCropTakesTheRectangleAsked(t *testing.T) {
	images := himage.NewImageManager()
	img := images.FromBytes(solidPNG(t, 40, 20, color.RGBA{A: 255})).Crop(8, 6, 2, 2)

	if w, h := size(t, img); w != 8 || h != 6 {
		t.Fatalf("crop = %dx%d, want 8x6", w, h)
	}
}

func TestFlipsAndRotations(t *testing.T) {
	images := himage.NewImageManager()
	source := stripedPNG(t, 4, 2)

	// The first column is black and the second white; mirrored, the last is
	// black.
	out, err := images.FromBytes(source).FlipHorizontally().ToBytes()
	if err != nil {
		t.Fatalf("ToBytes: %v", err)
	}
	canvas := decode(t, out)
	if got := canvas.RGBAAt(3, 0).R; got != 0 {
		t.Fatalf("last column = %d, want 0: the image was not mirrored", got)
	}

	// A quarter turn swaps the axes.
	turned := images.FromBytes(source).Rotate(90)
	if w, h := size(t, turned); w != 2 || h != 4 {
		t.Fatalf("rotated = %dx%d, want 2x4", w, h)
	}

	// Flop is the other name for the horizontal flip, Flip for the vertical
	// one.
	if _, err := images.FromBytes(source).Flip().Flop().ToBytes(); err != nil {
		t.Fatalf("Flip/Flop: %v", err)
	}
}

func TestRotateFillsTheCornersWithTheBackground(t *testing.T) {
	images := himage.NewImageManager()
	out, err := images.FromBytes(solidPNG(t, 20, 20, color.RGBA{B: 255, A: 255})).
		Rotate(45, "#ff0000").ToBytes()
	if err != nil {
		t.Fatalf("ToBytes: %v", err)
	}
	canvas := decode(t, out)
	if got := canvas.RGBAAt(0, 0); got.R < 250 || got.B > 5 {
		t.Fatalf("the corner is %v, want the red that was asked for", got)
	}
}

func TestGrayscaleUsesLuminance(t *testing.T) {
	images := himage.NewImageManager()
	out, err := images.FromBytes(solidPNG(t, 4, 4, color.RGBA{R: 255, A: 255})).Grayscale().ToBytes()
	if err != nil {
		t.Fatalf("ToBytes: %v", err)
	}
	got := decode(t, out).RGBAAt(1, 1)
	if got.R != got.G || got.G != got.B {
		t.Fatalf("pixel = %v, want a grey", got)
	}
	// Rec. 601 puts pure red at 0.299 of full brightness, not at half and not
	// at a third.
	if got.R < 72 || got.R > 80 {
		t.Fatalf("grey = %d, want about 76", got.R)
	}
}

func TestBlurAndSharpenRun(t *testing.T) {
	images := himage.NewImageManager()
	source := stripedPNG(t, 16, 16)

	blurred, err := images.FromBytes(source).Blur().ToBytes()
	if err != nil {
		t.Fatalf("Blur: %v", err)
	}
	got := decode(t, blurred).RGBAAt(8, 8).R
	if got < 60 || got > 195 {
		t.Fatalf("blurred pixel = %d, want the stripes mixed towards the middle", got)
	}

	if _, err := images.FromBytes(source).Sharpen().ToBytes(); err != nil {
		t.Fatalf("Sharpen: %v", err)
	}
}

func TestToFormatRefusesAnUnsupportedName(t *testing.T) {
	images := himage.NewImageManager()
	if _, err := images.FromBytes(solidPNG(t, 2, 2, color.RGBA{A: 255})).ToFormat("tiff"); !errors.Is(err, himage.ErrImage) {
		t.Fatalf("err = %v, want ErrImage", err)
	}
}

func TestHeifIsFoldedToHeic(t *testing.T) {
	images := himage.NewImageManager()
	img, err := images.FromBytes(solidPNG(t, 2, 2, color.RGBA{A: 255})).ToFormat("heif")
	if err != nil {
		t.Fatalf("ToFormat: %v", err)
	}
	_, err = img.ToBytes()
	if err == nil || !strings.Contains(err.Error(), "[heic]") {
		t.Fatalf("err = %v, want the failure to name heic", err)
	}
}

// TestAFormatWithNoEncoderFailsByName: ToWebp is accepted as a format name,
// and refused where the refusal can name the format rather than quietly
// handing back a JPEG.
func TestAFormatWithNoEncoderFailsByName(t *testing.T) {
	images := himage.NewImageManager()
	_, err := images.FromBytes(solidPNG(t, 2, 2, color.RGBA{A: 255})).ToWebp().ToBytes()
	if !errors.Is(err, himage.ErrImage) || !strings.Contains(err.Error(), "webp") {
		t.Fatalf("err = %v, want a failure naming webp", err)
	}
}

func TestConvertingToJpegChangesTheMimeTypeAndExtension(t *testing.T) {
	images := himage.NewImageManager()
	img := images.FromBytes(solidPNG(t, 8, 8, color.RGBA{R: 30, G: 60, B: 90, A: 255})).ToJpeg()

	mediaType, err := img.MimeType()
	if err != nil {
		t.Fatalf("MimeType: %v", err)
	}
	if mediaType != "image/jpeg" {
		t.Fatalf("mime = %s, want image/jpeg", mediaType)
	}
	extension, err := img.Extension()
	if err != nil {
		t.Fatalf("Extension: %v", err)
	}
	if extension != "jpg" {
		t.Fatalf("extension = %s, want jpg", extension)
	}
}

func TestHashNameIsInventedOnceAndCarriesTheExtension(t *testing.T) {
	images := himage.NewImageManager()
	img := images.FromBytes(solidPNG(t, 2, 2, color.RGBA{A: 255}))

	first, err := img.HashName()
	if err != nil {
		t.Fatalf("HashName: %v", err)
	}
	second, err := img.HashName()
	if err != nil {
		t.Fatalf("HashName: %v", err)
	}
	if first != second {
		t.Fatalf("HashName answered %q then %q; it must remember", first, second)
	}
	if !strings.HasSuffix(first, ".png") || len(first) != 44 {
		t.Fatalf("HashName = %q, want forty random characters and .png", first)
	}
	withPath, err := img.HashName("avatars")
	if err != nil {
		t.Fatalf("HashName: %v", err)
	}
	if withPath != "avatars/"+first {
		t.Fatalf("HashName(avatars) = %q, want the prefix in front", withPath)
	}
}

// TestStoreGoesThroughTheGrant verifies authorization at this package's edge:
// the Grant reaches the disk, which is what puts the tenant in the key.
func TestStoreGoesThroughTheGrant(t *testing.T) {
	images := himage.NewImageManager()
	disk := newFakeDisk()
	img := images.FromBytes(solidPNG(t, 4, 4, color.RGBA{A: 255}))

	key, err := img.Store(context.Background(), grant(), disk, "avatars")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if !strings.HasPrefix(key, "avatars/") || !strings.HasSuffix(key, ".png") {
		t.Fatalf("key = %q, want avatars/<hash>.png", key)
	}
	if disk.key != key {
		t.Fatalf("the disk was given %q and Store answered %q", disk.key, key)
	}
	if auth.Tenant(disk.grant) != tenant {
		t.Fatalf("the disk saw tenant %q, want %q", auth.Tenant(disk.grant), tenant)
	}
	if disk.contentType != "image/png" {
		t.Fatalf("content type = %q, want image/png", disk.contentType)
	}
}

func TestStoreAsUsesTheNameGiven(t *testing.T) {
	images := himage.NewImageManager()
	disk := newFakeDisk()
	img := images.FromBytes(solidPNG(t, 4, 4, color.RGBA{A: 255}))

	key, err := img.StoreAs(context.Background(), grant(), disk, "avatars/", "me.png")
	if err != nil {
		t.Fatalf("StoreAs: %v", err)
	}
	if key != "avatars/me.png" {
		t.Fatalf("key = %q, want avatars/me.png", key)
	}
}

func TestStorePubliclyMakesItPublic(t *testing.T) {
	images := himage.NewImageManager()
	disk := newFakeDisk()
	img := images.FromBytes(solidPNG(t, 4, 4, color.RGBA{A: 255}))

	if _, err := img.StorePubliclyAs(context.Background(), grant(), disk, "avatars", "me.png"); err != nil {
		t.Fatalf("StorePubliclyAs: %v", err)
	}
	if disk.visibility != "public" {
		t.Fatalf("visibility = %q, want public", disk.visibility)
	}
}

func TestFromStorageReadsBackWhatWasStored(t *testing.T) {
	images := himage.NewImageManager()
	disk := newFakeDisk()
	source := solidPNG(t, 6, 6, color.RGBA{G: 128, A: 255})

	if _, err := images.FromBytes(source).StoreAs(context.Background(), grant(), disk, "", "a.png"); err != nil {
		t.Fatalf("StoreAs: %v", err)
	}
	back, err := images.FromStorage(context.Background(), grant(), disk, "a.png")
	if err != nil {
		t.Fatalf("FromStorage: %v", err)
	}
	if w, h := size(t, back); w != 6 || h != 6 {
		t.Fatalf("read back %dx%d, want 6x6", w, h)
	}
}

func TestDominantColorAnswersTheAverage(t *testing.T) {
	images := himage.NewImageManager()
	got, err := images.FromBytes(solidPNG(t, 8, 8, color.RGBA{R: 0x10, G: 0x20, B: 0x30, A: 255})).DominantColor()
	if err != nil {
		t.Fatalf("DominantColor: %v", err)
	}
	if got != "#102030" {
		t.Fatalf("dominant = %s, want #102030", got)
	}
}

// TestDominantColorSamplesThePendingPipeline: the pipeline runs over a copy
// first, so the colour reports for the image as it will be, and leaves the
// image itself unprocessed.
func TestDominantColorSamplesThePendingPipeline(t *testing.T) {
	images := himage.NewImageManager()
	img := images.FromBytes(solidPNG(t, 8, 8, color.RGBA{R: 255, A: 255})).Grayscale()

	got, err := img.DominantColor()
	if err != nil {
		t.Fatalf("DominantColor: %v", err)
	}
	if got != "#4c4c4c" {
		t.Fatalf("dominant = %s, want the grey of the pending pipeline", got)
	}
	if w, h := size(t, img); w != 8 || h != 8 {
		t.Fatalf("the image was disturbed by the sampling: %dx%d", w, h)
	}
}

func TestToDataURICarriesTheMediaType(t *testing.T) {
	images := himage.NewImageManager()
	uri, err := images.FromBytes(solidPNG(t, 2, 2, color.RGBA{A: 255})).ToDataURI()
	if err != nil {
		t.Fatalf("ToDataURI: %v", err)
	}
	if !strings.HasPrefix(uri, "data:image/png;base64,") {
		t.Fatalf("uri = %.40q, want a png data URI", uri)
	}
	text, err := images.FromBytes(solidPNG(t, 2, 2, color.RGBA{A: 255})).ToString()
	if err != nil {
		t.Fatalf("ToString: %v", err)
	}
	if text != uri {
		t.Fatal("ToString and ToDataURI answered differently")
	}
}

func TestFromBase64RefusesRubbish(t *testing.T) {
	if _, err := himage.NewImageManager().FromBase64("not base64 at all!!"); !errors.Is(err, himage.ErrImage) {
		t.Fatalf("err = %v, want ErrImage", err)
	}
}

func TestAnUnreadableFormatIsRefusedByName(t *testing.T) {
	images := himage.NewImageManager()
	// A WebP header with nothing behind it: recognised, refused, named.
	webp := append([]byte("RIFF\x00\x00\x00\x00WEBP"), make([]byte, 16)...)
	_, err := images.FromBytes(webp).Grayscale().ToBytes()
	if !errors.Is(err, himage.ErrImage) || !strings.Contains(err.Error(), "image/webp") {
		t.Fatalf("err = %v, want a failure naming image/webp", err)
	}
}

func TestFromUploadKeepsTheFile(t *testing.T) {
	images := himage.NewImageManager()
	upload := fakeUpload{name: "sideways.jpg", body: solidPNG(t, 4, 4, color.RGBA{A: 255})}

	img, err := images.FromUpload(upload)
	if err != nil {
		t.Fatalf("FromUpload: %v", err)
	}
	if img.File() == nil || img.File().GetClientOriginalName() != "sideways.jpg" {
		t.Fatal("the upload was not kept on the image")
	}
	if images.FromBytes(nil).File() != nil {
		t.Fatal("an image built from bytes has no upload and must say so")
	}
}

type fakeUpload struct {
	name string
	body []byte
}

func (f fakeUpload) GetContent() ([]byte, error)   { return f.body, nil }
func (f fakeUpload) GetClientOriginalName() string { return f.name }

func TestFromURLReadsTheBody(t *testing.T) {
	images := himage.NewImageManager()
	body := solidPNG(t, 5, 5, color.RGBA{A: 255})
	client := fakeClient{status: http.StatusOK, body: body}

	img, err := images.FromURL(context.Background(), client, "https://example.test/a.png")
	if err != nil {
		t.Fatalf("FromURL: %v", err)
	}
	if w, _ := size(t, img); w != 5 {
		t.Fatalf("width = %d, want 5", w)
	}

	failing := fakeClient{status: http.StatusNotFound, body: nil}
	if _, err := images.FromURL(context.Background(), failing, "https://example.test/a.png"); !errors.Is(err, himage.ErrImage) {
		t.Fatalf("err = %v, want ErrImage for a 404", err)
	}
}

type fakeClient struct {
	status int
	body   []byte
}

func (c fakeClient) Do(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: c.status,
		Status:     http.StatusText(c.status),
		Body:       io.NopCloser(bytes.NewReader(c.body)),
		Header:     http.Header{},
	}, nil
}

// TestTransformUsingReplacesWhatTheDriverWouldDo verifies that a registered
// handler is keyed by the transformation's name and replaces what the driver
// would otherwise do.
func TestTransformUsingReplacesWhatTheDriverWouldDo(t *testing.T) {
	images := himage.NewImageManager()
	called := false
	images.TransformUsing(himage.StdDriverName, "Grayscale",
		func(canvas *image.RGBA, _ transformations.Transformation) (*image.RGBA, error) {
			called = true
			return canvas, nil
		})

	out, err := images.FromBytes(solidPNG(t, 4, 4, color.RGBA{R: 255, A: 255})).Grayscale().ToBytes()
	if err != nil {
		t.Fatalf("ToBytes: %v", err)
	}
	if !called {
		t.Fatal("the handler was not called")
	}
	if got := decode(t, out).RGBAAt(1, 1); got.R != 255 || got.G != 0 {
		t.Fatalf("pixel = %v, want the red the handler left alone", got)
	}
}

func TestAnUnknownDriverIsNamedInTheError(t *testing.T) {
	if _, err := himage.NewImageManager().Driver("imagick"); !errors.Is(err, himage.ErrImage) ||
		!strings.Contains(err.Error(), "imagick") {
		t.Fatalf("err = %v, want a failure naming imagick", err)
	}
}

func TestExtendRegistersADriver(t *testing.T) {
	images := himage.NewImageManager()
	images.Extend("fake", func() (himage.Driver, error) { return fakeDriver{}, nil })
	images.SetDefaultDriver("fake")

	if images.GetDefaultDriver() != "fake" {
		t.Fatalf("default driver = %s, want fake", images.GetDefaultDriver())
	}
	out, err := images.FromBytes(solidPNG(t, 2, 2, color.RGBA{A: 255})).Grayscale().ToBytes()
	if err != nil {
		t.Fatalf("ToBytes: %v", err)
	}
	if string(out) != "processed" {
		t.Fatalf("out = %q, want the registered driver's answer", out)
	}
}

type fakeDriver struct{}

func (fakeDriver) Process([]byte, *himage.ImagePipeline) ([]byte, error) {
	return []byte("processed"), nil
}
func (fakeDriver) Dimensions([]byte) (int, int, error)  { return 1, 1, nil }
func (fakeDriver) DominantColor([]byte) (string, error) { return "#000000", nil }
func (d fakeDriver) TransformUsing(string, himage.TransformationHandler) himage.Driver {
	return d
}
func (d fakeDriver) MaxPixels(int) himage.Driver { return d }

func TestToResponseWritesTheImage(t *testing.T) {
	images := himage.NewImageManager()
	img := images.FromBytes(solidPNG(t, 3, 3, color.RGBA{A: 255}))

	rec := &recorder{header: http.Header{}}
	if err := img.ToResponse(rec, nil); err != nil {
		t.Fatalf("ToResponse: %v", err)
	}
	if got := rec.header.Get("Content-Type"); got != "image/png" {
		t.Fatalf("content type = %q, want image/png", got)
	}
	if rec.status != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.status)
	}
	if len(rec.body) == 0 {
		t.Fatal("no body was written")
	}
}

type recorder struct {
	header http.Header
	status int
	body   []byte
}

func (r *recorder) Header() http.Header { return r.header }
func (r *recorder) Write(b []byte) (int, error) {
	r.body = append(r.body, b...)
	return len(b), nil
}
func (r *recorder) WriteHeader(status int) { r.status = status }

func TestThePipelineKnowsWhetherAnythingWasAsked(t *testing.T) {
	p := himage.NewImagePipeline()
	if p.HasChanges() {
		t.Fatal("an empty pipeline reports changes")
	}
	p.Add(transformations.Grayscale{})
	if !p.HasChanges() {
		t.Fatal("a pipeline with a transformation reports none")
	}

	q := himage.NewImagePipeline()
	q.Output.Quality = 50
	if !q.HasChanges() {
		t.Fatal("a quality is a change and must be reported as one")
	}
	if !q.Output.HasChanges() {
		t.Fatal("ImageOutputOptions.HasChanges disagrees with the pipeline")
	}
}

func TestQualityIsClampedToTheValidRange(t *testing.T) {
	images := himage.NewImageManager()
	source := solidPNG(t, 32, 32, color.RGBA{R: 90, G: 140, B: 210, A: 255})

	low, err := images.FromBytes(source).ToJpg().Quality(-5).ToBytes()
	if err != nil {
		t.Fatalf("ToBytes: %v", err)
	}
	high, err := images.FromBytes(source).ToJpg().Quality(500).ToBytes()
	if err != nil {
		t.Fatalf("ToBytes: %v", err)
	}
	if len(low) >= len(high) {
		t.Fatalf("quality 1 produced %d bytes and quality 100 produced %d", len(low), len(high))
	}
}

func TestOptimizeSetsBothFormatAndQuality(t *testing.T) {
	images := himage.NewImageManager()
	img, err := images.FromBytes(solidPNG(t, 8, 8, color.RGBA{A: 255})).Optimize("jpg", 40)
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	mediaType, err := img.MimeType()
	if err != nil {
		t.Fatalf("MimeType: %v", err)
	}
	if mediaType != "image/jpeg" {
		t.Fatalf("mime = %s, want image/jpeg", mediaType)
	}
	if _, err := images.FromBytes(nil).Optimize("nope", 40); !errors.Is(err, himage.ErrImage) {
		t.Fatalf("err = %v, want ErrImage for an unknown format", err)
	}
}

func TestWidthAndHeightAnswerFromTheProcessedImage(t *testing.T) {
	images := himage.NewImageManager()
	img := images.FromBytes(solidPNG(t, 40, 20, color.RGBA{A: 255})).Cover(12, 6)

	w, err := img.Width()
	if err != nil {
		t.Fatalf("Width: %v", err)
	}
	h, err := img.Height()
	if err != nil {
		t.Fatalf("Height: %v", err)
	}
	if w != 12 || h != 6 {
		t.Fatalf("%dx%d, want 12x6", w, h)
	}
}

func TestUsingNamesTheDriverForOneImageOnly(t *testing.T) {
	images := himage.NewImageManager()
	images.Extend("fake", func() (himage.Driver, error) { return fakeDriver{}, nil })

	img := images.FromBytes(solidPNG(t, 2, 2, color.RGBA{A: 255}))
	out, err := img.Using("fake").Grayscale().ToBytes()
	if err != nil {
		t.Fatalf("ToBytes: %v", err)
	}
	if string(out) != "processed" {
		t.Fatalf("out = %q, want the named driver's answer", out)
	}
	if _, err := img.Grayscale().ToBytes(); err != nil {
		t.Fatalf("the default driver stopped working: %v", err)
	}
}

// craftedPNG is a valid PNG file of about seventy bytes that declares w by h
// and carries nothing like that many pixels.
//
// This is the attack, written out: the header is free to write, and the canvas
// the decoder allocates from it is not. The IDAT holds a real but tiny zlib
// stream, which matters -- image/png allocates the whole image the moment it
// reaches an IDAT and only afterwards finds there is not enough pixel data
// behind it. A file with no IDAT at all would be refused by the decoder for
// nothing more than ending early, and would prove nothing about a ceiling.
func craftedPNG(t *testing.T, w, h int) []byte {
	t.Helper()

	chunk := func(name string, data []byte) []byte {
		body := append([]byte(name), data...)
		out := binary.BigEndian.AppendUint32(nil, uint32(len(data)))
		out = append(out, body...)
		// The CRC is real because image/png verifies it before it reads the
		// dimensions out of the chunk.
		return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(body))
	}

	header := binary.BigEndian.AppendUint32(nil, uint32(w))
	header = binary.BigEndian.AppendUint32(header, uint32(h))
	header = append(header, 8, 6, 0, 0, 0) // eight bits a channel, colour with alpha

	var pixels bytes.Buffer
	zw := zlib.NewWriter(&pixels)
	if _, err := zw.Write(make([]byte, 64)); err != nil {
		t.Fatalf("zlib.Write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zlib.Close: %v", err)
	}

	out := []byte("\x89PNG\r\n\x1a\n")
	out = append(out, chunk("IHDR", header)...)
	out = append(out, chunk("IDAT", pixels.Bytes())...)
	return append(out, chunk("IEND", nil)...)
}

// allocatedDuring is how many bytes the heap was asked for while f ran.
//
// TotalAlloc is cumulative and never falls, so the difference is what f asked
// for whether or not the collector took it back -- which is the question here,
// since a canvas that is allocated and immediately dropped is still a canvas
// somebody else chose the size of.
func allocatedDuring(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// TestAnOversizedDeclarationIsRefusedBeforeThePixelsAreAllocated is the whole
// point of the ceiling, and the allocation is the assertion.
//
// A test that decoded the image and then measured it would have proved nothing:
// the memory is spent inside the decoder, before anything this package wrote
// gets a look at the result. Seventy-odd bytes here declare 144 megapixels, and
// the canvas image/png allocates for them, before it discovers there is no
// pixel data behind them, is 549 MiB.
func TestAnOversizedDeclarationIsRefusedBeforeThePixelsAreAllocated(t *testing.T) {
	images := himage.NewImageManager()
	bomb := craftedPNG(t, 12000, 12000)

	var err error
	allocated := allocatedDuring(func() {
		_, err = images.FromBytes(bomb).Grayscale().ToBytes()
	})

	if !errors.Is(err, himage.ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	if !errors.Is(err, himage.ErrImage) {
		t.Fatalf("err = %v, want ErrImage too: one check must still catch every failure of this package", err)
	}
	if !strings.Contains(err.Error(), "12000") ||
		!strings.Contains(err.Error(), strconv.Itoa(himage.DefaultMaxPixels)) {
		t.Fatalf("err = %v, want the declared dimensions and the ceiling named", err)
	}
	if allocated > 1<<20 {
		t.Fatalf("the refusal allocated %d bytes, and the canvas it refused is %d: the pixels were decoded first",
			allocated, 4*12000*12000)
	}
}

// TestTheCeilingIsWhatTheManagerWasTold: the limit is configurable in both
// directions, and reaches a driver that was already built -- the second call
// lands after the first has created one.
func TestTheCeilingIsWhatTheManagerWasTold(t *testing.T) {
	images := himage.NewImageManager().MaxPixels(100)
	source := solidPNG(t, 20, 20, color.RGBA{A: 255}) // four hundred pixels

	_, err := images.FromBytes(source).Grayscale().ToBytes()
	if !errors.Is(err, himage.ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge under a ceiling of 100 pixels", err)
	}

	images.MaxPixels(0)
	if _, err := images.FromBytes(source).Grayscale().ToBytes(); err != nil {
		t.Fatalf("ToBytes: %v; zero must put the default ceiling back", err)
	}
}

// TestTheCeilingCoversTheDominantColourToo: it is the other method that decodes,
// and a ceiling that only covered the pipeline would leave the same file
// costing the same memory by another name.
func TestTheCeilingCoversTheDominantColourToo(t *testing.T) {
	images := himage.NewImageManager()

	var err error
	allocated := allocatedDuring(func() {
		_, err = images.FromBytes(craftedPNG(t, 12000, 12000)).DominantColor()
	})

	if !errors.Is(err, himage.ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	if allocated > 1<<20 {
		t.Fatalf("the refusal allocated %d bytes: the pixels were decoded first", allocated)
	}
}

// TestAnImageTooLargeToDecodeIsStillStoredAndMeasured states the edge of the
// ceiling rather than leaving it to be discovered. It bounds the decode, and
// nothing else: an upload nobody asked to transform is never decoded, so it is
// stored as it arrived, and its header is read at any size it likes.
func TestAnImageTooLargeToDecodeIsStillStoredAndMeasured(t *testing.T) {
	images := himage.NewImageManager()
	disk := newFakeDisk()
	bomb := craftedPNG(t, 12000, 12000)

	key, err := images.FromBytes(bomb).StoreAs(context.Background(), grant(), disk, "uploads", "big.png")
	if err != nil {
		t.Fatalf("StoreAs: %v", err)
	}
	if key != "uploads/big.png" || !bytes.Equal(disk.body, bomb) {
		t.Fatalf("stored %q with %d bytes, want the file exactly as it arrived", key, len(disk.body))
	}

	w, h, err := images.FromBytes(bomb).Dimensions()
	if err != nil {
		t.Fatalf("Dimensions: %v", err)
	}
	if w != 12000 || h != 12000 {
		t.Fatalf("dimensions = %dx%d, want the 12000x12000 the header declares", w, h)
	}
}
