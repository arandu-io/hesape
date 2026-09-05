package raster

import (
	"bytes"
	"context"
	"encoding/xml"
	stdimage "image"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/fyne-io/oksvg"
	"github.com/srwiley/rasterx"
	"github.com/srwiley/scanFT"
)

func svgDimensions(ctx context.Context, b []byte, o Options) (int, int, error) {
	if len(b) > 1<<20 {
		return 0, 0, fail("SVG exceeds 1 MiB")
	}
	decoder := xml.NewDecoder(bytes.NewReader(b))
	depth, count, roots, metadataDepth := 0, 0, 0, 0
	var width, height float64
	for {
		if err := ctx.Err(); err != nil {
			return 0, 0, err
		}
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, 0, fail("invalid SVG XML: %v", err)
		}
		switch t := token.(type) {
		case xml.Directive:
			// XML does not resolve external DTDs. Internal subsets/entities are
			// refused, while exported SVG 1.1 DOCTYPE declarations remain usable.
			if strings.ContainsAny(string(t), "[]") || !strings.HasPrefix(string(t), "DOCTYPE svg ") {
				return 0, 0, fail("unsupported SVG directive")
			}
		case xml.ProcInst:
			if t.Target != "xml" {
				return 0, 0, fail("unsupported SVG processing instruction")
			}
		case xml.StartElement:
			if metadataDepth > 0 {
				return 0, 0, fail("nested SVG metadata is not supported")
			}
			depth++
			count++
			if t.Name.Local == "metadata" {
				metadataDepth = depth
			}
			if depth > 64 || count > 10000 {
				return 0, 0, fail("SVG complexity limit exceeded")
			}
			if t.Name.Space != "" && t.Name.Space != "http://www.w3.org/2000/svg" {
				return 0, 0, fail("unsupported SVG namespace")
			}
			if depth == 1 {
				roots++
				if roots != 1 || t.Name.Local != "svg" {
					return 0, 0, fail("expected one SVG root")
				}
				width, height, err = viewport(t.Attr)
				if err != nil {
					return 0, 0, err
				}
			} else if t.Name.Local == "svg" {
				return 0, 0, fail("nested SVG is not supported")
			}
			switch t.Name.Local {
			case "svg", "g", "defs", "style", "title", "desc", "metadata", "path", "rect", "circle", "ellipse", "line", "polyline", "polygon", "linearGradient", "radialGradient", "stop":
			default:
				return 0, 0, fail("unsupported SVG element [%s]", t.Name.Local)
			}
			for _, a := range t.Attr {
				switch a.Name.Local {
				case "filter", "clip-path", "mask", "display", "visibility":
					return 0, 0, fail("unsupported SVG attribute [%s]", a.Name.Local)
				}
				if strings.HasPrefix(strings.ToLower(a.Name.Local), "on") || a.Name.Local == "href" {
					return 0, 0, fail("SVG event handlers and references are not supported")
				}
				if err := safeStyle(a.Value); err != nil {
					return 0, 0, err
				}
			}
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(t)) != "" {
				return 0, 0, fail("text outside SVG root")
			}
			if err := safeStyle(string(t)); err != nil {
				return 0, 0, err
			}
		case xml.EndElement:
			if depth == metadataDepth {
				metadataDepth = 0
			}
			depth--
		}
	}
	if roots != 1 || depth != 0 {
		return 0, 0, fail("incomplete SVG")
	}
	if o.SVGWidth > 0 {
		width, height = float64(o.SVGWidth), float64(o.SVGHeight)
	}
	if math.IsNaN(width) || math.IsNaN(height) || math.IsInf(width, 0) || math.IsInf(height, 0) || width <= 0 || height <= 0 || width > float64(o.MaxPixels) || height > float64(o.MaxPixels) {
		return 0, 0, fail("invalid SVG viewport")
	}
	w, h := int(math.Ceil(width)), int(math.Ceil(height))
	if err := checkPixels(w, h, o.MaxPixels); err != nil {
		return 0, 0, err
	}
	return w, h, nil
}

func safeStyle(s string) error {
	s = strings.ToLower(s)
	for _, property := range []string{"filter:", "clip-path:", "mask:", "display:", "visibility:"} {
		if strings.Contains(strings.ReplaceAll(s, " ", ""), property) {
			return fail("unsupported SVG style property")
		}
	}
	if strings.Contains(s, "@import") || strings.Contains(s, "javascript:") || strings.Contains(s, "\\") {
		return fail("unsupported SVG style")
	}
	for {
		i := strings.Index(s, "url(")
		if i < 0 {
			return nil
		}
		s = s[i+4:]
		j := strings.Index(s, ")")
		if j < 0 || !strings.HasPrefix(strings.Trim(strings.TrimSpace(s[:j]), "\"'"), "#") {
			return fail("external SVG resources are not supported")
		}
		s = s[j+1:]
	}
}

func viewport(attrs []xml.Attr) (float64, float64, error) {
	var w, h float64
	var box string
	for _, a := range attrs {
		switch a.Name.Local {
		case "viewBox":
			box = a.Value
		case "width":
			w, _ = strconv.ParseFloat(strings.TrimSuffix(a.Value, "px"), 64)
		case "height":
			h, _ = strconv.ParseFloat(strings.TrimSuffix(a.Value, "px"), 64)
		}
	}
	if box != "" {
		fields := strings.Fields(strings.ReplaceAll(box, ",", " "))
		if len(fields) != 4 {
			return 0, 0, fail("invalid SVG viewBox")
		}
		values := [4]float64{}
		for i, f := range fields {
			v, err := strconv.ParseFloat(f, 64)
			if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
				return 0, 0, fail("invalid SVG viewBox")
			}
			values[i] = v
		}
		w, h = values[2], values[3]
	}
	if w <= 0 || h <= 0 {
		return 0, 0, fail("SVG requires positive viewBox or px dimensions")
	}
	return w, h, nil
}

func renderSVG(ctx context.Context, b []byte, o Options) (result stdimage.Image, err error) {
	// Malformed path data must be an image error, never a process crash.
	defer func() {
		if v := recover(); v != nil {
			result = nil
			err = fail("SVG renderer rejected input: %v", v)
		}
	}()
	w, h, err := svgDimensions(ctx, b, o)
	if err != nil {
		return nil, err
	}
	prepared, nonZero, err := prepareSVG(b)
	if err != nil {
		return nil, err
	}
	icon, err := oksvg.ReadIconStream(bytes.NewReader(prepared), oksvg.StrictErrorMode)
	if err != nil {
		return nil, fail("parse SVG: %v", err)
	}
	if icon.ViewBox.W <= 0 || icon.ViewBox.H <= 0 {
		return nil, fail("invalid renderer viewport")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	canvas := stdimage.NewRGBA(stdimage.Rect(0, 0, w, h))
	// ScannerGV ignores even-odd winding. ScannerFT honors both SVG rules.
	scanner := scanFT.NewScannerFT(w, h, scanFT.NewRGBAPainter(canvas))
	raster := rasterx.NewDasher(w, h, scanner)
	// Scale the viewBox origin as well as its paths. SetTarget translates
	// before scaling, which misplaces a nonzero origin at resized dimensions.
	icon.Transform = rasterx.Identity.Scale(float64(w)/icon.ViewBox.W, float64(h)/icon.ViewBox.H).Translate(-icon.ViewBox.X, -icon.ViewBox.Y)
	for _, path := range icon.SVGPaths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path.UseNonZeroWinding = nonZero
		path.DrawTransformed(raster, 1, icon.Transform)
	}
	return canvas, nil
}

var fillRule = regexp.MustCompile(`(?i)fill-rule\s*:\s*([a-z]+)`)

// oksvg does not apply fill-rule declarations. Support a uniform explicit
// rule (common in exported logos), and refuse mixed rules instead of changing
// holes in the artwork. Non-rendering metadata is represented as description
// in the in-memory input; the caller's original bytes remain untouched.
func prepareSVG(b []byte) ([]byte, bool, error) {
	decoder := xml.NewDecoder(bytes.NewReader(b))
	var out bytes.Buffer
	encoder := xml.NewEncoder(&out)
	rule := "nonzero"
	setRule := func(value string, root bool) error {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "nonzero" && value != "evenodd" {
			return fail("unsupported SVG fill rule")
		}
		if !root && rule != value {
			return fail("mixed SVG fill rules are not supported")
		}
		rule = value
		return nil
	}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, false, err
		}
		switch t := token.(type) {
		case xml.StartElement:
			if t.Name.Local == "metadata" {
				t.Name.Local = "desc"
			}
			for _, a := range t.Attr {
				if a.Name.Local == "fill-rule" {
					if err := setRule(a.Value, t.Name.Local == "svg"); err != nil {
						return nil, false, err
					}
				}
				if a.Name.Local == "style" {
					for _, match := range fillRule.FindAllStringSubmatch(a.Value, -1) {
						if err := setRule(match[1], t.Name.Local == "svg"); err != nil {
							return nil, false, err
						}
					}
				}
			}
			token = t
		case xml.EndElement:
			if t.Name.Local == "metadata" {
				t.Name.Local = "desc"
			}
			token = t
		case xml.CharData:
			for _, match := range fillRule.FindAllStringSubmatch(string(t), -1) {
				if err := setRule(match[1], false); err != nil {
					return nil, false, err
				}
			}
		}
		if err := encoder.EncodeToken(token); err != nil {
			return nil, false, err
		}
	}
	if err := encoder.Flush(); err != nil {
		return nil, false, err
	}
	return out.Bytes(), rule != "evenodd", nil
}
