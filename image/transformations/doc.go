// Package transformations is Illuminate\Image\Transformations: the twelve
// things that can be asked of an image, as twelve types.
//
// It answers, one for one, to the files in the clone at
// laravel_illuminate/image/Transformations, tag v13.25.0:
//
//	Blur.php  Contain.php  Cover.php  Crop.php
//	FlipHorizontally.php  FlipVertically.php  Grayscale.php  Orient.php
//	Resize.php  Rotate.php  Scale.php  Sharpen.php
//
// Each is a record and nothing else -- Illuminate's are readonly constructor
// properties, and these are exported fields, which is the same thing said in
// Go. What they mean is decided by the driver that executes them, and the
// driver in the image package is the one that decides it here.
//
// They live in their own package for one reason: the image package builds them,
// so if [Transformation] lived there, this package could not name it and the
// import would be a cycle. Illuminate puts the interface in Contracts\Image,
// which is the same separation with a different address.
//
// A caller rarely names these types. Illuminate\Image\Image has a method for
// each -- Cover, Blur, Orient -- and they are the way in; what these are for is
// Image.Transform, and the custom transformation that a driver was taught to
// handle with ImageManager.TransformUsing.
package transformations
