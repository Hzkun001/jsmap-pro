package main

import (
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/parse/v2/js"
)

const maxAST = 25 << 20

func variable(v *js.Var) *js.Var {
	for v != nil && v.Link != nil {
		v = v.Link
	}
	return v
}

// walkSyntax omits parser scope/binding metadata, which contains cycles.
// Interface slots are passed by address so a CallExpr can become a LiteralExpr.
func walkSyntax(v reflect.Value, visit func(reflect.Value)) {
	if !v.IsValid() {
		return
	}
	if v.Kind() == reflect.Interface {
		if v.IsNil() {
			return
		}
		visit(v)
		walkSyntax(v.Elem(), visit)
		return
	}
	if v.Type() == reflect.TypeOf(js.Scope{}) || v.Type() == reflect.TypeOf((*js.Scope)(nil)) || v.Type() == reflect.TypeOf((*js.Var)(nil)) {
		return
	}
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			walkSyntax(v.Elem(), visit)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).Name != "Scope" {
				walkSyntax(v.Field(i), visit)
			}
		}
	case reflect.Slice:
		if v.Type().Elem().Kind() != reflect.Uint8 {
			for i := 0; i < v.Len(); i++ {
				walkSyntax(v.Index(i), visit)
			}
		}
	}
}

type nodeVisitor func(js.INode)

func (v nodeVisitor) Enter(n js.INode) js.IVisitor { v(n); return v }
func (v nodeVisitor) Exit(js.INode)                {}

func inlineStrings(reader io.Reader) (string, int, error) {
	tree, err := js.Parse(parse.NewInput(reader), js.Options{})
	if err != nil {
		return "", 0, err
	}
	arrays := map[*js.Var][]*js.LiteralExpr{}
	// shortcut: only a static declaration prefix is eligible; general control-flow analysis is required for later declarations.
	for _, stmt := range tree.List {
		if _, ok := stmt.(*js.FuncDecl); ok {
			continue
		}
		decl, ok := stmt.(*js.VarDecl)
		if !ok {
			break
		}
		valid := true
		for _, binding := range decl.List {
			name, ok := binding.Binding.(*js.Var)
			if !ok {
				valid = false
				break
			}
			array, ok := binding.Default.(*js.ArrayExpr)
			if !ok {
				valid = false
				break
			}
			values := []*js.LiteralExpr{}
			for _, element := range array.List {
				literal, ok := element.Value.(*js.LiteralExpr)
				if !ok || literal.TokenType != js.StringToken || element.Spread {
					valid = false
					break
				}
				values = append(values, literal)
			}
			if !valid {
				break
			}
			arrays[variable(name)] = values
		}
		if !valid {
			break
		}
	}
	getters := map[*js.Var]*js.Var{}
	for _, stmt := range tree.List {
		f, ok := stmt.(*js.FuncDecl)
		if !ok || f.Async || f.Generator || f.Name == nil || len(f.Params.List) != 1 || f.Params.Rest != nil || len(f.Body.List) != 1 {
			continue
		}
		param, ok := f.Params.List[0].Binding.(*js.Var)
		if !ok || f.Params.List[0].Default != nil {
			continue
		}
		ret, ok := f.Body.List[0].(*js.ReturnStmt)
		if !ok {
			continue
		}
		index, ok := ret.Value.(*js.IndexExpr)
		if !ok || index.Optional {
			continue
		}
		arr, ok := index.X.(*js.Var)
		if !ok {
			continue
		}
		arg, ok := index.Y.(*js.Var)
		if !ok || variable(arg) != variable(param) {
			continue
		}
		if _, ok := arrays[variable(arr)]; ok {
			getters[variable(f.Name)] = variable(arr)
		}
	}
	refs, calls := map[*js.Var]int{}, map[*js.Var]int{}
	dynamic := false
	js.Walk(nodeVisitor(func(n js.INode) {
		switch n := n.(type) {
		case *js.Var:
			refs[variable(n)]++
			if name := string(n.Name()); name == "eval" || name == "Function" || name == "globalThis" || name == "window" || name == "self" {
				dynamic = true
			}
		case *js.LiteralExpr:
			if n.TokenType == js.ThisToken {
				dynamic = true
			}
		case *js.WithStmt:
			dynamic = true
		case *js.CallExpr:
			if v, ok := n.X.(*js.Var); ok {
				calls[variable(v)]++
			}
		}
	}), tree)
	if dynamic {
		return "", 0, nil
	}
	for getter, array := range getters {
		// Extra references can mutate, alias, redeclare, or escape the array/getter.
		if refs[array] != 2 || refs[getter] != calls[getter]+1 {
			delete(getters, getter)
		}
	}
	changed := 0
	walkSyntax(reflect.ValueOf(tree), func(slot reflect.Value) {
		call, ok := slot.Interface().(*js.CallExpr)
		if !ok || call.Optional || len(call.Args.List) != 1 || call.Args.List[0].Rest {
			return
		}
		getter, ok := call.X.(*js.Var)
		if !ok {
			return
		}
		array, ok := getters[variable(getter)]
		if !ok {
			return
		}
		literal, ok := call.Args.List[0].Value.(*js.LiteralExpr)
		if !ok || !js.IsNumeric(literal.TokenType) {
			return
		}
		digits := strings.ReplaceAll(string(literal.Data), "_", "")
		if len(digits) > 1 && digits[0] == '0' && digits[1] >= '0' && digits[1] <= '9' {
			return
		} // Legacy octal is ambiguous outside strict mode.
		number, err := strconv.ParseFloat(digits, 64)
		if strings.HasPrefix(strings.ToLower(digits), "0x") || strings.HasPrefix(strings.ToLower(digits), "0o") || strings.HasPrefix(strings.ToLower(digits), "0b") {
			n, e := strconv.ParseUint(digits, 0, 64)
			number, err = float64(n), e
		}
		if err != nil || number < 0 || number >= float64(len(arrays[array])) || number != float64(int(number)) {
			return
		}
		if slot.CanSet() {
			slot.Set(reflect.ValueOf(arrays[array][int(number)]))
			changed++
		}
	})
	if changed == 0 {
		return "", 0, nil
	}
	return tree.JSString(), changed, nil
}

func analyzeAST(root *os.Root, r *FileResult, emit func(Finding)) {
	f, err := root.Open(r.File)
	if err != nil {
		r.Error = err.Error()
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		r.Error = err.Error()
		return
	}
	if info.Size() > maxAST {
		r.AnalysisNote = "AST skipped: file exceeds 25 MiB; raw streaming scan completed"
		return
	}
	transformed, count, err := inlineStrings(f)
	if err != nil {
		r.AnalysisNote = "AST parse skipped: " + err.Error()
		return
	}
	if count == 0 {
		return
	}
	filename := "analyzed/" + strings.TrimPrefix(r.File, "raw/")
	if err := mkdirAllRoot(root, "analyzed", 0700); err != nil {
		r.Error = err.Error()
		return
	}
	if err := writeFile(root, filename, strings.NewReader(transformed)); err != nil {
		r.Error = err.Error()
		return
	}
	_, err = scanJS(strings.NewReader(transformed), func(hit Finding) { hit.File = filename; r.Findings = append(r.Findings, hit); emit(hit) })
	if err != nil {
		r.Error = err.Error()
		return
	}
	r.AnalysisNote = fmt.Sprintf("Inlined %d static getter calls; transformed findings reference %s", count, filename)
}
