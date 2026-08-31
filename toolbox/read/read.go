package read

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strings"

	"crdx.org/io/internal/file"

	"crdx.org/io/internal/util"
	"crdx.org/io/internal/util/imageutil"
	"crdx.org/io/internal/util/strutil"
	"crdx.org/io/tool"
)

const maxFileBytes = 20 * 1024 * 1024

var errFileTooLarge = errors.New("file is too large to read")

type loadedFile struct {
	data      []byte
	mediaType string
	size      int64
}

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

	loadedFile, err := load(root, name)
	stats := tool.Stats{Kind: tool.StatsRead, Bytes: loadedFile.size}
	if errors.Is(err, errFileTooLarge) {
		if imageutil.IsSupported(loadedFile.mediaType) && (args.Offset > 0 || args.Limit > 0) {
			return tool.ToolCallResult{Stats: stats}, errors.New("line ranges are not supported for images")
		}
		noun := "file"
		if imageutil.IsSupported(loadedFile.mediaType) {
			noun = "image"
		}
		return tool.ToolCallResult{Stats: stats}, fmt.Errorf("%s is larger than the %d-byte limit", noun, maxFileBytes)
	}
	if err != nil {
		if pathError, ok := errors.AsType[*fs.PathError](err); ok {
			return tool.ToolCallResult{}, fmt.Errorf("%s: %w", args.Path, pathError.Err)
		}

		return tool.ToolCallResult{}, err
	}

	data := loadedFile.data
	mediaType := loadedFile.mediaType
	if imageutil.IsSupported(mediaType) {
		if args.Offset > 0 || args.Limit > 0 {
			return tool.ToolCallResult{Stats: stats}, errors.New("line ranges are not supported for images")
		}

		stats.Kind = tool.StatsImage
		if width, height, ok := imageutil.Dimensions(data); ok {
			stats.EstimatedTokens = util.EstimateImageTokenCount(imageutil.Fit(width, height))
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

func load(root *file.Root, name string) (loadedFile, error) {
	openedFile, err := root.Open(name)
	if err != nil {
		return loadedFile{}, err
	}
	defer func() { _ = openedFile.Close() }()

	info, err := openedFile.Stat()
	if err != nil {
		return loadedFile{}, err
	}
	if info.Size() > maxFileBytes {
		header, err := io.ReadAll(io.LimitReader(openedFile, 512))
		if err != nil {
			return loadedFile{}, err
		}
		return loadedFile{
			mediaType: http.DetectContentType(header),
			size:      info.Size(),
		}, errFileTooLarge
	}

	data, err := io.ReadAll(io.LimitReader(openedFile, maxFileBytes+1))
	if err != nil {
		return loadedFile{}, err
	}
	mediaType := http.DetectContentType(data)
	if len(data) > maxFileBytes {
		return loadedFile{
			mediaType: mediaType,
			size:      max(info.Size(), int64(len(data))),
		}, errFileTooLarge
	}

	return loadedFile{data: data, mediaType: mediaType, size: int64(len(data))}, nil
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
