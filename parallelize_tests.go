package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == "vendor" || info.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}

		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			fmt.Printf("Error parsing %s: %v\n", path, err)
			return nil
		}

		type insert struct {
			line int
			name string
		}
		var insertLines []insert

		ast.Inspect(f, func(n ast.Node) bool {
			// Find func TestX(t *testing.T)
			if fd, ok := n.(*ast.FuncDecl); ok {
				if strings.HasPrefix(fd.Name.Name, "Test") {
					if fd.Type.Params != nil && len(fd.Type.Params.List) == 1 {
						p := fd.Type.Params.List[0]
						if starExpr, ok := p.Type.(*ast.StarExpr); ok {
							if selExpr, ok := starExpr.X.(*ast.SelectorExpr); ok {
								if ident, ok := selExpr.X.(*ast.Ident); ok && ident.Name == "testing" && selExpr.Sel.Name == "T" {
									tName := "t"
									if len(p.Names) > 0 {
										tName = p.Names[0].Name
									}
									
									hasParallel := false
									for _, stmt := range fd.Body.List {
										if exprStmt, ok := stmt.(*ast.ExprStmt); ok {
											if call, ok := exprStmt.X.(*ast.CallExpr); ok {
												if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
													if sel.Sel.Name == "Parallel" {
														hasParallel = true
													}
												}
											}
										}
									}
									if !hasParallel && tName != "_" {
										insertLines = append(insertLines, insert{
											line: fset.Position(fd.Body.Lbrace).Line,
											name: tName,
										})
									}
								}
							}
						}
					}
				}
			}

			// Find t.Run("name", func(t *testing.T) { ... })
			if call, ok := n.(*ast.CallExpr); ok {
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Run" {
					if len(call.Args) == 2 {
						if funcLit, ok := call.Args[1].(*ast.FuncLit); ok {
							if funcLit.Type.Params != nil && len(funcLit.Type.Params.List) == 1 {
								p := funcLit.Type.Params.List[0]
								if starExpr, ok := p.Type.(*ast.StarExpr); ok {
									if selExpr, ok := starExpr.X.(*ast.SelectorExpr); ok {
										if ident, ok := selExpr.X.(*ast.Ident); ok && ident.Name == "testing" && selExpr.Sel.Name == "T" {
											tName := "t"
											if len(p.Names) > 0 {
												tName = p.Names[0].Name
											}

											hasParallel := false
											for _, stmt := range funcLit.Body.List {
												if exprStmt, ok := stmt.(*ast.ExprStmt); ok {
													if c2, ok := exprStmt.X.(*ast.CallExpr); ok {
														if s2, ok := c2.Fun.(*ast.SelectorExpr); ok {
															if s2.Sel.Name == "Parallel" {
																hasParallel = true
															}
														}
													}
												}
											}
											if !hasParallel && tName != "_" {
												insertLines = append(insertLines, insert{
													line: fset.Position(funcLit.Body.Lbrace).Line,
													name: tName,
												})
											}
										}
									}
								}
							}
						}
					}
				}
			}
			return true
		})

		if len(insertLines) > 0 {
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			lines := strings.Split(string(content), "\n")
			var newLines []string
			insertMap := make(map[int]string)
			for _, ins := range insertLines {
				insertMap[ins.line] = ins.name
			}

			for i, line := range lines {
				newLines = append(newLines, line)
				if name, ok := insertMap[i+1]; ok {
					newLines = append(newLines, "\t"+name+".Parallel()")
				}
			}

			err = os.WriteFile(path, []byte(strings.Join(newLines, "\n")), 0644)
			if err != nil {
				return err
			}
			fmt.Printf("Updated %s\n", path)
		}

		return nil
	})
	if err != nil {
		fmt.Println(err)
	}
}
