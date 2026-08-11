package image

import "github.com/arandu-io/hesape/image/transformations"

// ImagePipeline answers Illuminate\Image\ImagePipeline: the transformations to
// run, in the order they were asked for, and what to encode the result as.
//
// It is the whole of what [Image] accumulates. Nothing here touches a pixel --
// the pipeline is a plan, and the [Driver] is what executes it, once, when
// somebody finally asks for bytes.
type ImagePipeline struct {
	// Transformations is Illuminate's public $transformations, in order.
	Transformations []transformations.Transformation
	// Output is Illuminate's public $output. It is a value and not a pointer
	// because a pipeline that shared its options with the pipeline it was
	// cloned from would let a quality set on one image reach another, which is
	// the bug Illuminate's __clone() exists to prevent.
	Output ImageOutputOptions
}

// NewImagePipeline answers ImagePipeline::__construct().
func NewImagePipeline() *ImagePipeline { return &ImagePipeline{} }

// Add answers ImagePipeline::add().
func (p *ImagePipeline) Add(t transformations.Transformation) {
	p.Transformations = append(p.Transformations, t)
}

// HasChanges answers ImagePipeline::hasChanges(): whether anything at all was
// asked of this image.
func (p *ImagePipeline) HasChanges() bool {
	return len(p.Transformations) > 0 || p.Output.HasChanges()
}

// clone answers ImagePipeline::__clone(). The slice is copied rather than
// shared: two images that grew from one call to Blur must not append into the
// same backing array.
func (p *ImagePipeline) clone() *ImagePipeline {
	out := &ImagePipeline{Output: p.Output}
	if len(p.Transformations) > 0 {
		out.Transformations = make([]transformations.Transformation, len(p.Transformations))
		copy(out.Transformations, p.Transformations)
	}
	return out
}
