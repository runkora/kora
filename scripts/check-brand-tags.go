//go:build ignore

// check-brand-tags fails when a struct tag in a speaking package names the product (ruleguard cannot match tags).
// Run from the repository root: go run scripts/check-brand-tags.go
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	roots = []string{
		"backend/internal/agents",
		"backend/internal/koramcp",
		"backend/internal/egressgw",
		"backend/internal/platform/sandboximages",
	}
	productNameWord = regexp.MustCompile(`(?i)(^|[^-_./a-z0-9])(kora|orbit)(\.[^a-z0-9]|[^-_./a-z0-9]|$)`)
)

func main() {
	fset := token.NewFileSet()
	var findings []string
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			ast.Inspect(file, func(n ast.Node) bool {
				field, ok := n.(*ast.Field)
				if !ok || field.Tag == nil {
					return true
				}
				tag, err := strconv.Unquote(field.Tag.Value)
				if err != nil {
					tag = field.Tag.Value
				}
				if productNameWord.MatchString(tag) {
					findings = append(findings, fmt.Sprintf("%s: struct tag names the product: %s", fset.Position(field.Tag.Pos()), tag))
				}
				return true
			})
			return nil
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "check-brand-tags:", err)
			os.Exit(2)
		}
	}
	if len(findings) > 0 {
		for _, f := range findings {
			fmt.Fprintln(os.Stderr, f)
		}
		fmt.Fprintln(os.Stderr, "\nA struct tag cannot read the brand, so its text must be neutral, or move to code that takes the product name (see backend/internal/koramcp/tools_agents.go). docs/adr/2026-10-12-white-labelling-theme.md, Decision 5.")
		os.Exit(1)
	}
}
