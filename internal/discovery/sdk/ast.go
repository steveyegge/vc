package sdk

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// GoFile represents a parsed Go source file with convenient accessors.
type GoFile struct {
	Path    string
	Node    *ast.File
	FileSet *token.FileSet
	Package string
}

// WalkGoFiles walks a directory tree and calls the visitor function for each Go file.
//
// Example:
//
//	sdk.WalkGoFiles("/path/to/project", func(file *sdk.GoFile) error {
//		for _, fn := range file.Functions() {
//			// Process each function
//		}
//		return nil
//	})
func WalkGoFiles(rootPath string, visitor func(*GoFile) error) error {
	return WalkGoFilesWithOptions(rootPath, WalkOptions{}, visitor)
}

// WalkOptions configures the file walk behavior.
type WalkOptions struct {
	// ExcludeDirs specifies directory names to skip (e.g., "vendor", ".git")
	ExcludeDirs []string

	// IncludeTests includes *_test.go files (default: false)
	IncludeTests bool

	// IncludeGenerated includes *_generated.go files (default: false)
	IncludeGenerated bool

	// ParseComments includes comments in the AST (default: false)
	ParseComments bool
}

// DefaultWalkOptions returns walk options with common exclusions.
func DefaultWalkOptions() WalkOptions {
	return WalkOptions{
		ExcludeDirs:      []string{"vendor", ".git", "node_modules"},
		IncludeTests:     false,
		IncludeGenerated: false,
		ParseComments:    false,
	}
}

// WalkGoFilesWithOptions walks Go files with custom options.
func WalkGoFilesWithOptions(rootPath string, opts WalkOptions, visitor func(*GoFile) error) error {
	return filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip excluded directories
		if info.IsDir() {
			for _, exclude := range opts.ExcludeDirs {
				if strings.Contains(path, string(filepath.Separator)+exclude+string(filepath.Separator)) ||
					strings.HasSuffix(path, string(filepath.Separator)+exclude) {
					return filepath.SkipDir
				}
			}
			return nil
		}

		// Skip non-Go files
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		// Skip test files if not included
		if !opts.IncludeTests && strings.HasSuffix(path, "_test.go") {
			return nil
		}

		// Skip generated files if not included
		if !opts.IncludeGenerated && strings.Contains(path, "_generated.go") {
			return nil
		}

		// Parse the file
		goFile, err := ParseGoFile(path, opts.ParseComments)
		if err != nil {
			// Skip files that fail to parse
			return nil
		}

		// Call visitor
		return visitor(goFile)
	})
}

// ParseGoFile parses a single Go file and returns a GoFile wrapper.
func ParseGoFile(path string, includeComments bool) (*GoFile, error) {
	fset := token.NewFileSet()

	mode := parser.Mode(0)
	if includeComments {
		mode = parser.ParseComments
	}

	node, err := parser.ParseFile(fset, path, nil, mode)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	return &GoFile{
		Path:    path,
		Node:    node,
		FileSet: fset,
		Package: node.Name.Name,
	}, nil
}

// Functions returns all function declarations in the file.
func (f *GoFile) Functions() []*Function {
	var functions []*Function

	for _, decl := range f.Node.Decls {
		if funcDecl, ok := decl.(*ast.FuncDecl); ok {
			functions = append(functions, &Function{
				Node:    funcDecl,
				FileSet: f.FileSet,
				File:    f,
			})
		}
	}

	return functions
}

// Types returns all type declarations in the file.
func (f *GoFile) Types() []*TypeDecl {
	var types []*TypeDecl

	for _, decl := range f.Node.Decls {
		if genDecl, ok := decl.(*ast.GenDecl); ok && genDecl.Tok == token.TYPE {
			for _, spec := range genDecl.Specs {
				if typeSpec, ok := spec.(*ast.TypeSpec); ok {
					types = append(types, &TypeDecl{
						Node:    typeSpec,
						FileSet: f.FileSet,
						File:    f,
					})
				}
			}
		}
	}

	return types
}

// Imports returns all import declarations in the file.
func (f *GoFile) Imports() []string {
	var imports []string

	for _, imp := range f.Node.Imports {
		importPath := strings.Trim(imp.Path.Value, "\"")
		imports = append(imports, importPath)
	}

	return imports
}

// LineCount returns the number of lines in the file.
func (f *GoFile) LineCount() int {
	pos := f.FileSet.Position(f.Node.End())
	return pos.Line
}

// Function represents a function declaration with helper methods.
type Function struct {
	Node    *ast.FuncDecl
	FileSet *token.FileSet
	File    *GoFile
}

// Name returns the function name.
func (f *Function) Name() string {
	return f.Node.Name.Name
}

// IsMethod returns true if this is a method (has a receiver).
func (f *Function) IsMethod() bool {
	return f.Node.Recv != nil
}

// ReceiverType returns the receiver type name for methods.
func (f *Function) ReceiverType() string {
	if !f.IsMethod() {
		return ""
	}

	if len(f.Node.Recv.List) == 0 {
		return ""
	}

	return exprToString(f.Node.Recv.List[0].Type)
}

// LineCount returns the number of lines in the function body.
func (f *Function) LineCount() int {
	if f.Node.Body == nil {
		return 0
	}

	start := f.FileSet.Position(f.Node.Body.Pos())
	end := f.FileSet.Position(f.Node.Body.End())
	return end.Line - start.Line + 1
}

// StartLine returns the starting line number.
func (f *Function) StartLine() int {
	return f.FileSet.Position(f.Node.Pos()).Line
}

// EndLine returns the ending line number.
func (f *Function) EndLine() int {
	return f.FileSet.Position(f.Node.End()).Line
}

// Parameters returns the function parameters.
func (f *Function) Parameters() []Parameter {
	if f.Node.Type.Params == nil {
		return nil
	}

	var params []Parameter
	for _, field := range f.Node.Type.Params.List {
		typeStr := exprToString(field.Type)
		if len(field.Names) == 0 {
			// Unnamed parameter
			params = append(params, Parameter{
				Name: "",
				Type: typeStr,
			})
		} else {
			for _, name := range field.Names {
				params = append(params, Parameter{
					Name: name.Name,
					Type: typeStr,
				})
			}
		}
	}

	return params
}

// Returns returns the function return types.
func (f *Function) Returns() []string {
	if f.Node.Type.Results == nil {
		return nil
	}

	var returns []string
	for _, field := range f.Node.Type.Results.List {
		typeStr := exprToString(field.Type)
		if len(field.Names) == 0 {
			returns = append(returns, typeStr)
		} else {
			for range field.Names {
				returns = append(returns, typeStr)
			}
		}
	}

	return returns
}

// HasReturnType checks if the function returns a specific type.
func (f *Function) HasReturnType(typeName string) bool {
	for _, ret := range f.Returns() {
		if strings.Contains(ret, typeName) {
			return true
		}
	}
	return false
}

// CallsFunction checks if this function calls another function by name.
func (f *Function) CallsFunction(funcName string) bool {
	found := false

	ast.Inspect(f.Node, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == funcName {
				found = true
				return false
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == funcName {
				found = true
				return false
			}
		}
		return !found
	})

	return found
}

// Parameter represents a function parameter.
type Parameter struct {
	Name string
	Type string
}

// TypeDecl represents a type declaration with helper methods.
type TypeDecl struct {
	Node    *ast.TypeSpec
	FileSet *token.FileSet
	File    *GoFile
}

// Name returns the type name.
func (t *TypeDecl) Name() string {
	return t.Node.Name.Name
}

// IsStruct returns true if this is a struct type.
func (t *TypeDecl) IsStruct() bool {
	_, ok := t.Node.Type.(*ast.StructType)
	return ok
}

// IsInterface returns true if this is an interface type.
func (t *TypeDecl) IsInterface() bool {
	_, ok := t.Node.Type.(*ast.InterfaceType)
	return ok
}

// Fields returns struct fields (only for struct types).
func (t *TypeDecl) Fields() []Field {
	structType, ok := t.Node.Type.(*ast.StructType)
	if !ok {
		return nil
	}

	var fields []Field
	for _, field := range structType.Fields.List {
		typeStr := exprToString(field.Type)
		if len(field.Names) == 0 {
			// Embedded field
			fields = append(fields, Field{
				Name: "",
				Type: typeStr,
			})
		} else {
			for _, name := range field.Names {
				fields = append(fields, Field{
					Name: name.Name,
					Type: typeStr,
				})
			}
		}
	}

	return fields
}

// Methods returns interface methods (only for interface types).
func (t *TypeDecl) Methods() []Method {
	interfaceType, ok := t.Node.Type.(*ast.InterfaceType)
	if !ok {
		return nil
	}

	var methods []Method
	for _, method := range interfaceType.Methods.List {
		if len(method.Names) == 0 {
			// Embedded interface
			continue
		}

		if funcType, ok := method.Type.(*ast.FuncType); ok {
			for _, name := range method.Names {
				methods = append(methods, Method{
					Name:    name.Name,
					Params:  extractParams(funcType.Params),
					Returns: extractReturns(funcType.Results),
				})
			}
		}
	}

	return methods
}

// Field represents a struct field.
type Field struct {
	Name string
	Type string
}

// Method represents an interface method.
type Method struct {
	Name    string
	Params  []string
	Returns []string
}

// exprToString converts an AST expression to a string representation.
func exprToString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return fmt.Sprintf("%s.%s", exprToString(t.X), t.Sel.Name)
	case *ast.StarExpr:
		return "*" + exprToString(t.X)
	case *ast.ArrayType:
		return "[]" + exprToString(t.Elt)
	case *ast.MapType:
		return fmt.Sprintf("map[%s]%s", exprToString(t.Key), exprToString(t.Value))
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.FuncType:
		return "func"
	case *ast.ChanType:
		return "chan " + exprToString(t.Value)
	default:
		return "unknown"
	}
}

// extractParams extracts parameter types from a function.
func extractParams(params *ast.FieldList) []string {
	if params == nil {
		return nil
	}

	var result []string
	for _, param := range params.List {
		typeStr := exprToString(param.Type)
		if len(param.Names) == 0 {
			result = append(result, typeStr)
		} else {
			for range param.Names {
				result = append(result, typeStr)
			}
		}
	}

	return result
}

// extractReturns extracts return types from a function.
func extractReturns(results *ast.FieldList) []string {
	if results == nil {
		return nil
	}

	var result []string
	for _, ret := range results.List {
		typeStr := exprToString(ret.Type)
		if len(ret.Names) == 0 {
			result = append(result, typeStr)
		} else {
			for range ret.Names {
				result = append(result, typeStr)
			}
		}
	}

	return result
}
