package tool

// Image is visual content a call hands back for the model to inspect.
type Image struct {
	MediaType string // the MIME type of the encoded image
	Data      []byte // the encoded image bytes
}
