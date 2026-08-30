package messages

import (
	"encoding/base64"
	"encoding/json"

	"crdx.org/io/internal/util/imageutil"
	"crdx.org/io/tool"
)

func boundImageHistory(items []json.RawMessage) []json.RawMessage {
	boundedItems := make([]json.RawMessage, len(items))
	for index, item := range items {
		boundedItems[index] = boundJSONImages(item)
	}

	return boundedItems
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
			if key == "source" {
				if source, isSource := item.(map[string]any); isSource && boundImageSource(source) {
					wasBounded = true
					continue
				}
			}
			boundedItem, itemWasBounded := boundValueImages(item)
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

func boundImageSource(source map[string]any) bool {
	mediaType, hasMediaType := source["media_type"].(string)
	encoded, hasData := source["data"].(string)
	if !hasMediaType || !hasData || !imageutil.IsSupported(mediaType) {
		return false
	}

	boundedImage, wasBounded := boundEncodedImage(mediaType, encoded)
	if !wasBounded {
		return false
	}

	source["media_type"] = boundedImage.MediaType
	source["data"] = base64.StdEncoding.EncodeToString(boundedImage.Data)

	return true
}

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
