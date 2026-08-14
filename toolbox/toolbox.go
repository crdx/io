package toolbox

import (
	"os"

	"crdx.org/io/tool"
	"crdx.org/io/toolbox/edit"
	"crdx.org/io/toolbox/find"
	"crdx.org/io/toolbox/grep"
	"crdx.org/io/toolbox/ls"
	"crdx.org/io/toolbox/read"
	"crdx.org/io/toolbox/write"
)

// Files builds every file tool confined to root. readOnly leaves write and edit out.
func Files(root *os.Root, readOnly bool) []tool.Tool {
	built := []tool.Tool{
		read.New(root),
		ls.New(root),
		find.New(root),
		grep.New(root),
	}

	if !readOnly {
		built = append(built, write.New(root), edit.New(root))
	}

	return built
}
