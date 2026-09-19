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

var PathToolNames = []string{"read", "ls", "find", "grep", "write", "edit"}

func Rummage(root *file.Root, snapshots *file.Snapshots, isReadable func() bool) []tool.Tool {
	return []tool.Tool{
		read.New(root, snapshots, isReadable),
		ls.New(root, isReadable),
		find.New(root, isReadable),
		grep.New(root, snapshots, isReadable),
		write.New(root, snapshots),
		edit.New(root, snapshots),
	}
}
