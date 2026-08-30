package imagehistory

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"crdx.org/io/internal/util/imageutil"
	"crdx.org/io/tool"
)

type Cache struct {
	preparedItems []json.RawMessage
}

func (self *Cache) Prepare(items []json.RawMessage) []json.RawMessage {
	if len(items) < len(self.preparedItems) {
		self.Reset()
	}

	for _, item := range items[len(self.preparedItems):] {
		self.preparedItems = append(self.preparedItems, boundJSONImages(item))
	}
	return self.preparedItems
}

func (self *Cache) Reset() {
	self.preparedItems = nil
}

func boundJSONImages(payload []byte) []byte {
	var value any
	if json.Unmarshal(payload, &value) != nil {
		return payload
	}

	boundedValue, wasBounded := boundValueImages(value)
	if !wasBounded {
		return payload
	}

	encoded, err := json.Marshal(boundedValue)
	if err != nil {
		return payload
	}

	return encoded
}

func boundValueImages(value any) (any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		wasBounded := false
		for key, item := range typed {
			var boundedItem any
			var itemWasBounded bool
			if key == "image_url" {
				boundedItem, itemWasBounded = boundImageURL(item)
			} else {
				boundedItem, itemWasBounded = boundValueImages(item)
			}
			typed[key] = boundedItem
			wasBounded = wasBounded || itemWasBounded
		}
		return typed, wasBounded
	case []any:
		wasBounded := false
		for index, item := range typed {
			boundedItem, itemWasBounded := boundValueImages(item)
			typed[index] = boundedItem
			wasBounded = wasBounded || itemWasBounded
		}
		return typed, wasBounded
	}

	return value, false
}

func boundImageURL(value any) (any, bool) {
	switch typed := value.(type) {
	case string:
		boundedURL, wasBounded := boundDataURL(typed)
		if wasBounded {
			return boundedURL, true
		}
	case map[string]any:
		dataURL, hasURL := typed["url"].(string)
		if !hasURL {
			return value, false
		}
		boundedURL, wasBounded := boundDataURL(dataURL)
		if wasBounded {
			typed["url"] = boundedURL
			return typed, true
		}
	}
	return value, false
}

func boundDataURL(dataURL string) (string, bool) {
	mediaType, encoded, isDataURL := strings.Cut(dataURL, dataURLSeparator)
	if !isDataURL {
		return "", false
	}

	mediaType = strings.TrimPrefix(mediaType, dataURLPrefix)
	if !imageutil.IsSupported(mediaType) {
		return "", false
	}

	boundedImage, wasBounded := boundEncodedImage(mediaType, encoded)
	if !wasBounded {
		return "", false
	}

	return dataURLPrefix + boundedImage.MediaType + dataURLSeparator +
		base64.StdEncoding.EncodeToString(boundedImage.Data), true
}

const (
	dataURLPrefix    = "data:"
	dataURLSeparator = ";base64,"
)

func boundEncodedImage(mediaType string, encoded string) (tool.Image, bool) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return tool.Image{}, false
	}

	subject := tool.Image{MediaType: mediaType, Data: data}
	boundedImage := imageutil.Bound(subject)
	if boundedImage.MediaType == subject.MediaType && len(boundedImage.Data) == len(subject.Data) {
		return tool.Image{}, false
	}

	return boundedImage, true
}
