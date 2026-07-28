package main

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

const (
	logicImportPrefix = "github.com/yunloli/aiferry/internal/logic/"
	serviceDirectory  = "internal/service"
)

func main() {
	files, err := filepath.Glob(filepath.Join(serviceDirectory, "*.go"))
	if err != nil {
		fail(err)
	}
	for _, path := range files {
		domain := strings.TrimSuffix(filepath.Base(path), ".go")
		if err = patchFile(path, logicImportPrefix+domain); err != nil {
			fail(err)
		}
	}
}

func patchFile(path, logicImport string) error {
	logicDirectory := filepath.Join("internal/logic", filepath.Base(logicImport))
	types, err := logicTypes(logicDirectory)
	if err != nil {
		return fmt.Errorf("find logic package for %s: %w", path, err)
	}
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, path, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	importName := "_"
	if referencesLogicType(file, types) {
		importName = "."
	}
	for _, imported := range file.Imports {
		if strings.Trim(imported.Path.Value, `"`) == logicImport {
			imported.Name = ast.NewIdent(importName)
			return writeFile(path, set, file)
		}
	}
	spec := &ast.ImportSpec{Name: ast.NewIdent(importName), Path: &ast.BasicLit{Kind: token.STRING, Value: fmt.Sprintf("%q", logicImport)}}
	file.Imports = append(file.Imports, spec)
	for _, decl := range file.Decls {
		group, ok := decl.(*ast.GenDecl)
		if ok && group.Tok == token.IMPORT {
			group.Specs = append(group.Specs, spec)
			return writeFile(path, set, file)
		}
	}
	file.Decls = append([]ast.Decl{&ast.GenDecl{Tok: token.IMPORT, Specs: []ast.Spec{spec}}}, file.Decls...)
	return writeFile(path, set, file)
}

func logicTypes(directory string) (map[string]struct{}, error) {
	packages, err := parser.ParseDir(token.NewFileSet(), directory, func(info os.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, 0)
	if err != nil {
		return nil, err
	}
	names := make(map[string]struct{})
	for _, pkg := range packages {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				group, ok := decl.(*ast.GenDecl)
				if !ok || group.Tok != token.TYPE {
					continue
				}
				for _, spec := range group.Specs {
					name := spec.(*ast.TypeSpec).Name.Name
					if ast.IsExported(name) {
						names[name] = struct{}{}
					}
				}
			}
		}
	}
	return names, nil
}

func referencesLogicType(file *ast.File, types map[string]struct{}) bool {
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if ok {
			_, found = types[identifier.Name]
		}
		return !found
	})
	return found
}

func writeFile(path string, set *token.FileSet, file *ast.File) error {
	output, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer output.Close()
	if err = format.Node(output, set, file); err != nil {
		return fmt.Errorf("format %s: %w", path, err)
	}
	return nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
