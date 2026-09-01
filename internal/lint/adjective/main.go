package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"slices"
	"strings"
	"unicode"

	"crdx.org/io/internal/lint/runner"
)

var irregularParticiples = []string{
	"born",
	"broken",
	"built",
	"chosen",
	"dealt",
	"drawn",
	"driven",
	"fallen",
	"felt",
	"forgotten",
	"frozen",
	"given",
	"grown",
	"held",
	"hidden",
	"kept",
	"known",
	"lost",
	"meant",
	"paid",
	"said",
	"sent",
	"shown",
	"sold",
	"spent",
	"spoken",
	"stolen",
	"swept",
	"taken",
	"thrown",
	"told",
	"torn",
	"woken",
	"worn",
	"written",
}

var wordsEndingInEd = []string{
	"bed",
	"bled",
	"bred",
	"breed",
	"creed",
	"deed",
	"embed",
	"exceed",
	"fed",
	"feed",
	"freed",
	"greed",
	"indeed",
	"led",
	"need",
	"proceed",
	"red",
	"seed",
	"shed",
	"sled",
	"sped",
	"speed",
	"succeed",
	"tweed",
	"weed",
	"wed",
}

func main() {
	runner.Main("adjective", analyse)
}

func analyse(file runner.File) []runner.Diagnostic {
	var diagnostics []runner.Diagnostic
	for _, name := range declaredNames(file.Syntax) {
		parts := words(name.Name)
		if len(parts) != 1 || !isParticiple(parts[0]) {
			continue
		}
		diagnostics = append(diagnostics, file.Report(name, fmt.Sprintf(
			"%s: say what was %s, since a name is a noun rather than an adjective", name.Name, parts[0],
		)))
	}
	return diagnostics
}

func isParticiple(word string) bool {
	if slices.Contains(irregularParticiples, word) {
		return true
	}
	if !strings.HasSuffix(word, "ed") || len(word) < 4 {
		return false
	}
	return !slices.Contains(wordsEndingInEd, word)
}

func declaredNames(file *ast.File) []*ast.Ident {
	var names []*ast.Ident
	add := func(candidates ...*ast.Ident) {
		for _, name := range candidates {
			if name != nil && name.Name != "_" {
				names = append(names, name)
			}
		}
	}

	ast.Inspect(file, func(node ast.Node) bool {
		switch typedNode := node.(type) {
		case *ast.GenDecl:
			return typedNode.Tok != token.CONST
		case *ast.InterfaceType:
			return false
		case *ast.ValueSpec:
			if !isBooleanType(typedNode.Type) {
				add(typedNode.Names...)
			}
		case *ast.Field:
			if !isBooleanType(typedNode.Type) {
				add(typedNode.Names...)
			}
		case *ast.AssignStmt:
			if typedNode.Tok != token.DEFINE {
				return true
			}
			for _, target := range typedNode.Lhs {
				if identifier, isIdentifier := target.(*ast.Ident); isIdentifier {
					add(identifier)
				}
			}
		case *ast.RangeStmt:
			for _, target := range []ast.Expr{typedNode.Key, typedNode.Value} {
				if identifier, isIdentifier := target.(*ast.Ident); isIdentifier {
					add(identifier)
				}
			}
		}
		return true
	})
	return names
}

func isBooleanType(expression ast.Expr) bool {
	identifier, isIdentifier := expression.(*ast.Ident)
	return isIdentifier && identifier.Name == "bool"
}

func words(name string) []string {
	var found []string
	var current []rune

	for _, letter := range name {
		if unicode.IsUpper(letter) && len(current) > 0 {
			found = append(found, strings.ToLower(string(current)))
			current = nil
		}
		current = append(current, letter)
	}
	if len(current) > 0 {
		found = append(found, strings.ToLower(string(current)))
	}
	return found
}
