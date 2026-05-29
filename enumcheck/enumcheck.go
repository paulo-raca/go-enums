// Package enumcheck provides a go/analysis analyzer that statically enforces the
// invariants of github.com/paulo-raca/go-enums/enum and, in particular, checks
// that switch statements over an enum type are exhaustive.
//
// It enforces three rules:
//
//  1. Enum shape. A type that embeds enum.StringEnum/IntEnum must embed exactly
//     that one base and nothing else, parameterised by itself
//     (type Suit struct{ enum.StringEnum[Suit] }).
//  2. Member declaration. enum.New / enum.NextInt may only appear as the direct
//     initialiser of a package-level var, that var may not be reassigned, and
//     members must be declared in the enum type's own package.
//  3. Switch exhaustiveness. In a switch over an enum type, every case must name
//     a member of that enum, and either all members are covered or a default
//     clause is present.
package enumcheck

import (
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/ast/inspector"
)

// enumPkgPath is the import path of the enum library this analyzer understands.
const enumPkgPath = "github.com/paulo-raca/go-enums/enum"

// membersFact records, on an enum type's TypeName object, the names of its
// member vars so switches in other packages can be checked.
type membersFact struct{ Names []string }

func (*membersFact) AFact() {}
func (f *membersFact) String() string {
	return "enumMembers(" + strings.Join(f.Names, ",") + ")"
}

var Analyzer = &analysis.Analyzer{
	Name:      "enumcheck",
	Doc:       "checks paulo-raca/go-enums invariants and switch exhaustiveness",
	Run:       run,
	Requires:  []*analysis.Analyzer{inspect.Analyzer},
	FactTypes: []analysis.Fact{(*membersFact)(nil)},
}

func run(pass *analysis.Pass) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	rel := func(t types.Type) string { return types.TypeString(t, types.RelativeTo(pass.Pkg)) }

	// --- Rule 1: identify (and validate) enum types defined in this package. ---
	localEnum := map[*types.TypeName]bool{}
	members := map[*types.TypeName][]string{}
	scope := pass.Pkg.Scope()
	for _, name := range scope.Names() {
		tn, ok := scope.Lookup(name).(*types.TypeName)
		if !ok || tn.IsAlias() {
			continue
		}
		named, ok := tn.Type().(*types.Named)
		if !ok {
			continue
		}
		st, ok := named.Underlying().(*types.Struct)
		if !ok {
			continue
		}
		idx, arg, kind := enumBase(st)
		if kind == "" {
			continue // not enum-related
		}
		// It is intended as an enum; enforce the shape.
		if st.NumFields() != 1 || idx != 0 || !st.Field(0).Anonymous() {
			pass.Reportf(tn.Pos(), "enum type %s must embed only enum.%s[%s] and nothing else", tn.Name(), kind, tn.Name())
			continue
		}
		if !types.Identical(arg, named) {
			pass.Reportf(tn.Pos(), "enum base must be parameterised by %s, not %s", tn.Name(), rel(arg))
			continue
		}
		localEnum[tn] = true
		members[tn] = nil
	}

	// --- Rule 2 (gather): members are top-level vars initialised by New/NextInt. ---
	goodInit := map[*ast.CallExpr]bool{}
	memberVars := map[*types.Var]bool{}
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, nameIdent := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					ce, _, arg := newCall(pass, vs.Values[i])
					if ce == nil {
						continue
					}
					goodInit[ce] = true
					tn := localEnumOf(arg, localEnum)
					if tn == nil {
						continue // cross-package target; reported in the scan below
					}
					if v, ok := pass.TypesInfo.Defs[nameIdent].(*types.Var); ok {
						memberVars[v] = true
						members[tn] = append(members[tn], nameIdent.Name)
					}
				}
			}
		}
	}

	// Export member sets so switches in importing packages can be checked.
	for tn := range localEnum {
		names := append([]string(nil), members[tn]...)
		sort.Strings(names)
		pass.ExportObjectFact(tn, &membersFact{Names: names})
	}

	lookupMembers := func(tn *types.TypeName) ([]string, bool) {
		if localEnum[tn] {
			names := append([]string(nil), members[tn]...)
			sort.Strings(names)
			return names, true
		}
		var f membersFact
		if pass.ImportObjectFact(tn, &f) {
			return f.Names, true
		}
		return nil, false
	}

	// isMember reports whether v is a package-level enum member var.
	isMember := func(v *types.Var) bool {
		if memberVars[v] {
			return true
		}
		named, ok := v.Type().(*types.Named)
		if !ok || v.Pkg() == nil {
			return false
		}
		tn := named.Obj()
		names, ok := lookupMembers(tn)
		if !ok || tn.Pkg() == nil || v.Pkg().Path() != tn.Pkg().Path() {
			return false
		}
		if v.Parent() != v.Pkg().Scope() { // must be package-level, not a local
			return false
		}
		return contains(names, v.Name())
	}

	// --- Rule 2 (placement): every New/NextInt call must be a top-level init,
	// and must target an enum type from this same package. ---
	insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
		ce := n.(*ast.CallExpr)
		got, fnName, arg := newCall(pass, ce)
		if got == nil {
			return
		}
		if !goodInit[ce] {
			pass.Reportf(ce.Pos(), "enum.%s must directly initialise a package-level var", fnName)
			return
		}
		if named, ok := arg.(*types.Named); ok && named.Obj().Pkg() != pass.Pkg {
			pass.Reportf(ce.Pos(), "enum members of %s must be declared in its own package", rel(arg))
		}
	})

	// --- Rule 2 (immutability): member vars must not be reassigned. ---
	insp.Preorder([]ast.Node{(*ast.AssignStmt)(nil)}, func(n ast.Node) {
		as := n.(*ast.AssignStmt)
		if as.Tok == token.DEFINE {
			return
		}
		for _, lhs := range as.Lhs {
			if v := varOf(pass, lhs); v != nil && isMember(v) {
				pass.Reportf(lhs.Pos(), "enum member %s must not be reassigned", v.Name())
			}
		}
	})

	// --- Rule 3: switch exhaustiveness. ---
	insp.Preorder([]ast.Node{(*ast.SwitchStmt)(nil)}, func(n ast.Node) {
		sw := n.(*ast.SwitchStmt)
		if sw.Tag == nil {
			return
		}
		named, ok := pass.TypesInfo.TypeOf(sw.Tag).(*types.Named)
		if !ok {
			return
		}
		tn := named.Obj()
		names, ok := lookupMembers(tn)
		if !ok {
			return // not an enum type
		}
		covered := map[string]bool{}
		hasDefault := false
		for _, clause := range sw.Body.List {
			cc := clause.(*ast.CaseClause)
			if len(cc.List) == 0 {
				hasDefault = true
				continue
			}
			for _, e := range cc.List {
				v := varOf(pass, e)
				if v == nil || !isMember(v) || v.Pkg().Path() != tn.Pkg().Path() || !contains(names, v.Name()) {
					pass.Reportf(e.Pos(), "case in switch on %s is not a registered member", rel(named))
					continue
				}
				covered[v.Name()] = true
			}
		}
		if hasDefault {
			return
		}
		var missing []string
		for _, m := range names {
			if !covered[m] {
				missing = append(missing, m)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			pass.Reportf(sw.Pos(), "non-exhaustive switch on %s: missing %s", rel(named), strings.Join(missing, ", "))
		}
	})

	return nil, nil
}

// enumBase finds the first field of st whose type is enum.StringEnum/IntEnum and
// returns its field index, the type argument, and the base kind ("StringEnum" or
// "IntEnum"). kind is "" if no such field exists.
func enumBase(st *types.Struct) (idx int, arg types.Type, kind string) {
	for i := 0; i < st.NumFields(); i++ {
		named, ok := st.Field(i).Type().(*types.Named)
		if !ok {
			continue
		}
		obj := named.Obj()
		if obj.Pkg() == nil || obj.Pkg().Path() != enumPkgPath {
			continue
		}
		if obj.Name() != "StringEnum" && obj.Name() != "IntEnum" {
			continue
		}
		if ta := named.TypeArgs(); ta != nil && ta.Len() == 1 {
			return i, ta.At(0), obj.Name()
		}
	}
	return 0, nil, ""
}

// newCall reports whether e is a call to enum.New or enum.NextInt, returning the
// call expression, the function name, and the first type argument (the enum type).
func newCall(pass *analysis.Pass, e ast.Expr) (*ast.CallExpr, string, types.Type) {
	ce, ok := astutil.Unparen(e).(*ast.CallExpr)
	if !ok {
		return nil, "", nil
	}
	fun := astutil.Unparen(ce.Fun)
	switch f := fun.(type) {
	case *ast.IndexExpr:
		fun = f.X
	case *ast.IndexListExpr:
		fun = f.X
	}
	var id *ast.Ident
	switch f := astutil.Unparen(fun).(type) {
	case *ast.SelectorExpr:
		id = f.Sel
	case *ast.Ident:
		id = f
	default:
		return nil, "", nil
	}
	fn, ok := pass.TypesInfo.Uses[id].(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != enumPkgPath {
		return nil, "", nil
	}
	if fn.Name() != "New" && fn.Name() != "NextInt" {
		return nil, "", nil
	}
	var arg types.Type
	if inst := pass.TypesInfo.Instances[id]; inst.TypeArgs != nil && inst.TypeArgs.Len() > 0 {
		arg = inst.TypeArgs.At(0)
	}
	return ce, fn.Name(), arg
}

// varOf resolves e (an identifier or selector) to the var it refers to, or nil.
func varOf(pass *analysis.Pass, e ast.Expr) *types.Var {
	var id *ast.Ident
	switch x := astutil.Unparen(e).(type) {
	case *ast.Ident:
		id = x
	case *ast.SelectorExpr:
		id = x.Sel
	default:
		return nil
	}
	v, _ := pass.TypesInfo.ObjectOf(id).(*types.Var)
	return v
}

// localEnumOf returns the local enum TypeName that arg names, or nil.
func localEnumOf(arg types.Type, localEnum map[*types.TypeName]bool) *types.TypeName {
	named, ok := arg.(*types.Named)
	if !ok {
		return nil
	}
	if localEnum[named.Obj()] {
		return named.Obj()
	}
	return nil
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
