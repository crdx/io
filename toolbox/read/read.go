package read

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io/fs"
	"net/http"
	"strings"

	"crdx.org/io/internal/file"

	"crdx.org/io/internal/util"
	"crdx.org/io/internal/util/strutil"
	"crdx.org/io/tool"
)

const maxImageBytes = 20 * 1024 * 1024

// Args is what a read takes. An absent offset or limit is zero, which means the whole file.
type Args struct {
	Path   string `json:"path"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
}

// New builds the read tool confined to root.
func New(root *file.Root, snapshots *file.Snapshots) tool.Tool {
	restoreReadState := func(payload json.RawMessage) error {
		return snapshots.RestoreReadState(root, payload)
	}

	return tool.Implement(
		tool.Definition{
			Name:        "read",
			Description: "read a text or image file",
			Schema: tool.Schema{
				tool.String("path", "file"),
				tool.Integer("offset", "first line, 1-indexed").Optional(),
				tool.Integer("limit", "max lines").Optional(),
			},
		},
		Describe,
	).
		State(file.FileReadState, restoreReadState).
		FocusPath().
		IsEmbarrassinglyParallel().
		ChangesNothing().
		Run(func(_ context.Context, args Args) (tool.ToolCallResult, error) {
			return exec(root, args)
		})
}

// Describe reports a read's subject and qualifier: the path, and the lines it asks for.
func Describe(args Args) (string, string) {
	return args.Path, span(args.Offset, args.Limit)
}

func span(offset int, limit int) string {
	switch {
	case offset > 0 && limit > 0:
		return fmt.Sprintf("%d-%d", offset, offset+limit-1)
	case offset > 0:
		return fmt.Sprintf("%d+", offset)
	case limit > 0:
		return fmt.Sprintf("1-%d", limit)
	default:
		return ""
	}
}

func exec(root *file.Root, args Args) (tool.ToolCallResult, error) {
	if args.Path == "" {
		return tool.ToolCallResult{}, errors.New("path is required")
	}

	root, name, err := root.Resolve(args.Path)
	if err != nil {
		return tool.ToolCallResult{}, err
	}

	data, err := root.ReadFile(name)
	if err != nil {
		if pathError, ok := errors.AsType[*fs.PathError](err); ok {
			return tool.ToolCallResult{}, fmt.Errorf("%s: %w", args.Path, pathError.Err)
		}

		return tool.ToolCallResult{}, err
	}

	stats := tool.Stats{Kind: tool.StatsRead, Bytes: int64(len(data))}
	mediaType := http.DetectContentType(data)
	if isSupportedImage(mediaType) {
		if args.Offset > 0 || args.Limit > 0 {
			return tool.ToolCallResult{Stats: stats}, errors.New("line ranges are not supported for images")
		}
		if len(data) > maxImageBytes {
			return tool.ToolCallResult{Stats: stats}, fmt.Errorf("image is larger than the %d-byte limit", maxImageBytes)
		}

		stats.Kind = tool.StatsImage
		if width, height, ok := imageDimensions(data, mediaType); ok {
			stats.EstimatedTokens = util.EstimateImageTokenCount(width, height)
		}

		return successfulResult(
			args.Path,
			data,
			fmt.Sprintf("%s image (%d bytes)", mediaType, len(data)),
			tool.Image{MediaType: mediaType, Data: data},
			stats,
		), nil
	}

	lines := strutil.Lines(string(data))
	stats.Lines = int64(len(lines))

	if args.Offset <= 0 && args.Limit <= 0 {
		return successfulResult(args.Path, data, string(data), tool.Image{}, stats), nil
	}

	start := max(args.Offset-1, 0)
	if len(lines) == 0 {
		return successfulResult(args.Path, data, "", tool.Image{}, stats), nil
	}
	if start >= len(lines) {
		return tool.ToolCallResult{Stats: stats}, fmt.Errorf(
			"offset %d is past the end of the file (%d lines)", args.Offset, len(lines),
		)
	}

	end := len(lines)
	if args.Limit > 0 && start+args.Limit < end {
		end = start + args.Limit
	}

	output := strings.Join(lines[start:end], "\n")
	stats.Lines = int64(end - start)
	stats.Bytes = int64(len(output))
	return successfulResult(args.Path, data, output, tool.Image{}, stats), nil
}

func successfulResult(
	path string,
	data []byte,
	output string,
	image tool.Image,
	stats tool.Stats,
) tool.ToolCallResult {
	state := file.EncodeReadState(file.NewReadSnapshot(path, data))

	return tool.ToolCallResult{
		State:  state,
		Output: output,
		Image:  image,
		Stats:  stats,
	}
}

func isSupportedImage(mediaType string) bool {
	switch mediaType {
	case "image/gif", "image/jpeg", "image/png", "image/webp":
		return true
	default:
		return false
	}
}

func imageDimensions(data []byte, mediaType string) (int, int, bool) {
	reader := bytes.NewReader(data)

	switch mediaType {
	case "image/gif":
		config, err := gif.DecodeConfig(reader)
		return config.Width, config.Height, err == nil
	case "image/jpeg":
		config, err := jpeg.DecodeConfig(reader)
		return config.Width, config.Height, err == nil
	case "image/png":
		config, err := png.DecodeConfig(reader)
		return config.Width, config.Height, err == nil
	case "image/webp":
		return webPDimensions(data)
	default:
		return 0, 0, false
	}
}

func webPDimensions(data []byte) (int, int, bool) {
	const riffHeaderLength = 12
	if len(data) < riffHeaderLength || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return 0, 0, false
	}

	for chunkOffset := riffHeaderLength; chunkOffset+8 <= len(data); {
		chunkSize := int(binary.LittleEndian.Uint32(data[chunkOffset+4 : chunkOffset+8]))
		chunkStart := chunkOffset + 8
		chunkEnd := chunkStart + chunkSize
		if chunkEnd > len(data) {
			return 0, 0, false
		}

		chunkData := data[chunkStart:chunkEnd]
		switch string(data[chunkOffset : chunkOffset+4]) {
		case "VP8 ":
			if len(chunkData) >= 10 && string(chunkData[3:6]) == "\x9d\x01\x2a" {
				width := int(binary.LittleEndian.Uint16(chunkData[6:8]) & 0x3fff)
				height := int(binary.LittleEndian.Uint16(chunkData[8:10]) & 0x3fff)
				return width, height, width > 0 && height > 0
			}
		case "VP8L":
			if len(chunkData) >= 5 && chunkData[0] == 0x2f {
				dimensions := binary.LittleEndian.Uint32(chunkData[1:5])
				width := int(dimensions&0x3fff) + 1
				height := int((dimensions>>14)&0x3fff) + 1
				return width, height, true
			}
		case "VP8X":
			if len(chunkData) >= 10 {
				width := littleEndianUint24(chunkData[4:7]) + 1
				height := littleEndianUint24(chunkData[7:10]) + 1
				return width, height, true
			}
		}

		chunkOffset = chunkEnd + chunkSize%2
	}

	return 0, 0, false
}

func littleEndianUint24(data []byte) int {
	return int(data[0]) | int(data[1])<<8 | int(data[2])<<16
}
