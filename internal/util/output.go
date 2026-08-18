package util

import (
	"fmt"
	"io"
)

// WriteWarningf writes a warning when an output destination is available.
func WriteWarningf(to io.Writer, format string, arguments ...any) {
	if to != nil {
		_, _ = fmt.Fprintf(to, "warning: "+format+"\n", arguments...)
	}
}
