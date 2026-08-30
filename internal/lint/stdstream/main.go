package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const message = "a library package minds its own business"

var applicationRoots = []string{"cmd", "internal"}

var standardStreams = []string{"Stdin", "Stdout", "Stderr"}

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
		if isApplication(filename) {
			continue
		}
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
			fmt.Printf("%s: %s (stdstream)\n", diagnostic.position, diagnostic.message)
			hasDiagnostics = true
		}
	}
	if hasDiagnostics {
		os.Exit(1)
	}
}

func isApplication(filename string) bool {
	root, _, _ := strings.Cut(filepath.ToSlash(filename), "/")
	return slices.Contains(applicationRoots, root)
}

func analyse(filename string, source []byte) ([]diagnostic, error) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, filename, source, 0)
	if err != nil {
		return nil, err
	}

	packageName, isImported := importedName(file)
	if !isImported {
		return nil, nil
	}

	var diagnostics []diagnostic
	ast.Inspect(file, func(node ast.Node) bool {
		selector, isSelector := node.(*ast.SelectorExpr)
		if !isSelector || !slices.Contains(standardStreams, selector.Sel.Name) {
			return true
		}
		if identifier, isIdentifier := selector.X.(*ast.Ident); isIdentifier && identifier.Name == packageName {
			diagnostics = append(diagnostics, diagnostic{
				position: fileSet.Position(selector.Pos()),
				message:  message,
			})
		}

		return true
	})

	return diagnostics, nil
}

func importedName(file *ast.File) (string, bool) {
	for _, specification := range file.Imports {
		path, err := strconv.Unquote(specification.Path.Value)
		if err != nil || path != "os" {
			continue
		}
		if specification.Name != nil {
			return specification.Name.Name, specification.Name.Name != "_"
		}

		return "os", true
	}

	return "", false
}
