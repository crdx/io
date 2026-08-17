package toolbox

import (
	"crdx.org/io/internal/file"
	"crdx.org/io/tool"
	"crdx.org/io/toolbox/edit"
	"crdx.org/io/toolbox/find"
	"crdx.org/io/toolbox/grep"
	"crdx.org/io/toolbox/ls"
	"crdx.org/io/toolbox/read"
	"crdx.org/io/toolbox/write"
)

// Rummage builds every file tool confined to root.
func Rummage(root *file.Root) []tool.Tool {
	return []tool.Tool{
		read.New(root),
		ls.New(root),
		find.New(root),
		grep.New(root),
		write.New(root),
		edit.New(root),
	}
}
