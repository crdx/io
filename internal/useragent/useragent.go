package useragent

import (
	"fmt"
	"runtime"
)

func Get() string {
	return fmt.Sprintf("oh (%s; %s)", runtime.GOOS, runtime.GOARCH)
}
