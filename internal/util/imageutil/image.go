package imageutil

import (
	"bytes"
	"container/list"
	"crypto/sha256"
	"image"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"sync"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"crdx.org/io/tool"
)

const MaxEdge = 1568

const jpegQuality = 85

const formatJPEG = "jpeg"

const (
	mediaTypeGIF  = "image/gif"
	mediaTypeJPEG = "image/jpeg"
	mediaTypePNG  = "image/png"
	mediaTypeWebP = "image/webp"
)

func IsSupported(mediaType string) bool {
	switch mediaType {
	case mediaTypeGIF, mediaTypeJPEG, mediaTypePNG, mediaTypeWebP:
		return true
	default:
		return false
	}
}

func Bound(img tool.Image) tool.Image {
	width, height, isMeasured := Dimensions(img.Data)
	if !isMeasured {
		return img
	}

	boundedWidth, boundedHeight := Fit(width, height)
	if boundedWidth == width && boundedHeight == height {
		return img
	}

	key := imageCacheKey{digest: sha256.Sum256(img.Data), mediaType: img.MediaType}
	if remembered, wasRemembered := bounded.get(key); wasRemembered {
		return remembered
	}

	decoded, format, err := image.Decode(bytes.NewReader(img.Data))
	if err != nil {
		return img
	}
	scaled, err := encode(downscale(decoded, boundedWidth, boundedHeight), format)
	if err != nil {
		return img
	}

	return bounded.store(key, scaled)
}

const (
	maxBoundedImageCacheBytes   = 128 * 1024 * 1024
	maxBoundedImageCacheEntries = 256
)

type imageCacheKey struct {
	digest    [sha256.Size]byte
	mediaType string
}

type cachedImage struct {
	image   tool.Image
	orderAt *list.Element
}

type boundedImageCache struct {
	mutex         sync.Mutex
	maximumBytes  int
	maximumImages int
	storedBytes   int
	images        map[imageCacheKey]cachedImage
	order         *list.List
}

func newBoundedImageCache(maximumBytes int, maximumImages int) *boundedImageCache {
	return &boundedImageCache{
		maximumBytes:  maximumBytes,
		maximumImages: maximumImages,
		images:        map[imageCacheKey]cachedImage{},
		order:         list.New(),
	}
}

func (self *boundedImageCache) get(key imageCacheKey) (tool.Image, bool) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	remembered, wasRemembered := self.images[key]
	if wasRemembered {
		self.order.MoveToBack(remembered.orderAt)
	}
	return remembered.image, wasRemembered
}

func (self *boundedImageCache) store(key imageCacheKey, image tool.Image) tool.Image {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if remembered, wasRemembered := self.images[key]; wasRemembered {
		self.order.MoveToBack(remembered.orderAt)
		return remembered.image
	}
	imageBytes := cap(image.Data)
	if imageBytes > self.maximumBytes || self.maximumImages == 0 {
		return image
	}

	for self.storedBytes+imageBytes > self.maximumBytes || len(self.images) >= self.maximumImages {
		oldest := self.order.Front()
		oldestKey := oldest.Value.(imageCacheKey) //nolint:forcetypeassert // this private list stores only imageCacheKey values
		self.order.Remove(oldest)
		self.storedBytes -= cap(self.images[oldestKey].image.Data)
		delete(self.images, oldestKey)
	}

	orderAt := self.order.PushBack(key)
	self.images[key] = cachedImage{image: image, orderAt: orderAt}
	self.storedBytes += imageBytes
	return image
}

var bounded = newBoundedImageCache(maxBoundedImageCacheBytes, maxBoundedImageCacheEntries)

func Fit(width int, height int) (int, int) {
	if width <= 0 || height <= 0 || (width <= MaxEdge && height <= MaxEdge) {
		return width, height
	}

	if width >= height {
		return MaxEdge, max(height*MaxEdge/width, 1)
	}

	return max(width*MaxEdge/height, 1), MaxEdge
}

func encode(subject image.Image, format string) (tool.Image, error) {
	var encoded bytes.Buffer

	if format == formatJPEG {
		if err := jpeg.Encode(&encoded, subject, &jpeg.Options{Quality: jpegQuality}); err != nil {
			return tool.Image{}, err
		}

		return tool.Image{MediaType: mediaTypeJPEG, Data: encoded.Bytes()}, nil
	}

	if err := png.Encode(&encoded, subject); err != nil {
		return tool.Image{}, err
	}

	return tool.Image{MediaType: mediaTypePNG, Data: encoded.Bytes()}, nil
}

func downscale(source image.Image, width int, height int) image.Image {
	target := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(target, target.Bounds(), source, source.Bounds(), draw.Src, nil)

	return target
}

func Dimensions(data []byte) (int, int, bool) {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	return config.Width, config.Height, err == nil
}
