package format

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/BurntSushi/toml"
)

var ErrNewer = errors.New("written by a newer build than this one")

func Check(found int, supportedFormat int) error {
	if found > supportedFormat {
		return fmt.Errorf("format %d was %w (this one reads format %d)", found, ErrNewer, supportedFormat)
	}

	return nil
}

// IsNewer reports whether err came of a file written by a newer build.
func IsNewer(err error) bool {
	return errors.Is(err, ErrNewer)
}

type header struct {
	Version int `json:"version" toml:"version"`
}

// ReadJSON reads the format number of a JSON document, treating an absent number as unnumbered.
func ReadJSON(data []byte) (int, error) {
	var read header
	if err := json.Unmarshal(data, &read); err != nil {
		return 0, err
	}

	return read.Version, nil
}

// ReadTOML reads the format number of a TOML document, treating an absent number as unnumbered.
func ReadTOML(data []byte) (int, error) {
	var read header
	if _, err := toml.Decode(string(data), &read); err != nil {
		return 0, err
	}

	return read.Version, nil
}
