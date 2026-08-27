package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
)

const expectedReceiverName = "self"

type diagnostic struct {
	position token.Position
	message  string
}

func main() {
	filenames := os.Args[1:]
	slices.Sort(filenames)

	hasDiagnostics := false
	for _, filename := range filenames {
		filename = filepath.Clean(filename)
		source, err := os.ReadFile(filename)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			hasDiagnostics = true
			continue
		}
		diagnostics, err := analyse(filename, source)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			hasDiagnostics = true
			continue
		}
		for _, diagnostic := range diagnostics {
			fmt.Printf("%s: %s (receivername)\n", diagnostic.position, diagnostic.message)
			hasDiagnostics = true
		}
	}
	if hasDiagnostics {
		os.Exit(1)
	}
}

func analyse(filename string, source []byte) ([]diagnostic, error) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, filename, source, 0)
	if err != nil {
		return nil, err
	}

	var diagnostics []diagnostic
	for _, declaration := range file.Decls {
		function, isFunction := declaration.(*ast.FuncDecl)
		if !isFunction || function.Recv == nil {
			continue
		}
		for _, field := range function.Recv.List {
			for _, receiver := range field.Names {
				if receiver.Name != expectedReceiverName {
					diagnostics = append(diagnostics, diagnostic{
						position: fileSet.Position(receiver.Pos()),
						message:  "method receiver must be named self",
					})
				}
			}
		}
	}
	return diagnostics, nil
}
