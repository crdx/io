package terminal

import (
	"encoding/base64"
	"fmt"
	"io"
)

// Copy writes an OSC 52 sequence asking the terminal to copy text to its clipboard.
func Copy(writer io.Writer, text string) error {
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	_, err := fmt.Fprintf(writer, "\x1b]52;c;%s\x07", encoded)
	return err
}
