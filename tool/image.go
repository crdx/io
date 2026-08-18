package tool

import "context"

// Image is visual content a call hands back for the model to inspect.
type Image struct {
	MediaType string // the MIME type of the encoded image
	Data      []byte // the encoded image bytes
}

// ImagedCall is a call that may attach an image after it has run.
type ImagedCall interface {
	Call
	Image() (Image, bool)
}

// AttachedImage returns the image a call produced, if any.
func AttachedImage(call Call) (Image, bool) {
	imagedCall, ok := call.(ImagedCall)
	if !ok {
		return Image{}, false
	}

	return imagedCall.Image()
}

type measuredImageCall struct {
	Call

	exec     func(context.Context) (string, Image, Statistics, error)
	stats    Statistics
	image    Image
	ran      bool
	hasImage bool
}

func (self *measuredImageCall) Exec(ctx context.Context) (string, error) {
	output, image, stats, err := self.exec(ctx)
	self.stats = stats
	self.image = image
	self.ran = true
	self.hasImage = image.MediaType != "" && len(image.Data) > 0
	return output, err
}

func (self *measuredImageCall) Statistics() (Statistics, bool) {
	return self.stats, self.ran
}

func (self *measuredImageCall) Image() (Image, bool) {
	return self.image, self.hasImage
}
