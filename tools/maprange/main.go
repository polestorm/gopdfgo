// Wypisuje pętle range po mapach w podanych katalogach pakietów (bez plików testowych).
package main

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"strings"
)

func main() {
	for _, dir := range os.Args[1:] {
		fset := token.NewFileSet()
		pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") }, 0)
		if err != nil {
			fmt.Println("ERR", dir, err)
			continue
		}
		for _, pkg := range pkgs {
			var files []*ast.File
			for _, f := range pkg.Files {
				files = append(files, f)
			}
			info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
			conf := types.Config{Importer: importer.ForCompiler(fset, "source", nil), Error: func(error) {}}
			conf.Check(dir, fset, files, info)
			for _, f := range files {
				ast.Inspect(f, func(n ast.Node) bool {
					rs, ok := n.(*ast.RangeStmt)
					if !ok {
						return true
					}
					if tv, ok := info.Types[rs.X]; ok && tv.Type != nil {
						if _, isMap := tv.Type.Underlying().(*types.Map); isMap {
							pos := fset.Position(rs.Pos())
							fmt.Printf("%s:%d\t%s\n", pos.Filename, pos.Line, tv.Type.String())
						}
					}
					return true
				})
			}
		}
	}
}
