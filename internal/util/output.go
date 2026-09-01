package util

import (
	"fmt"
	"io"
)

func WriteWarningf(to io.Writer, format string, arguments ...any) {
	if to != nil {
		_, _ = fmt.Fprintf(to, "warning: "+format+"\n", arguments...)
	}
}
