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
//     initialiser of a package-level var, that var may not be reassigned,
//     members must be declared in the enum type's own package, and enum.New's
//     value argument must be a compile-time constant.
//  3. Switch exhaustiveness. In a switch over an enum type, every case must name
//     a member of that enum, and either all members are covered or a default
//     clause is present.
//  4. Cast value sets. A call to enum.LookupAs / enum.As / enum.MustAs (or an
//     enum.SameValues assertion) between two enum types whose statically-known
//     backing value sets are not exactly equal is flagged, since some members
//     could not survive the cast.
package enumcheck

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/ast/inspector"
)

// enumPkgPath is the import path of the enum library this analyzer understands.
const enumPkgPath = "github.com/paulo-raca/go-enums/enum"

// membersFact records, on an enum type's TypeName object, the names of its
// member vars (so switches in other packages can be checked) plus the base
// kind and the statically-computed backing values (so cast sites can compare
// value sets across packages). Complete is false when some member's value is
// not a compile-time constant, in which case Values is unusable and cast-site
// checks involving the type are skipped.
type membersFact struct {
	Names    []string // sorted member var names
	Kind     string   // "StringEnum" or "IntEnum"
	Values   []string // sorted backing values (decimal for IntEnum)
	Complete bool
}

func (*membersFact) AFact() {}
func (f *membersFact) String() string {
	s := "enumMembers(" + strings.Join(f.Names, ",")
	if !f.Complete {
		return s + "; values unknown)"
	}
	return s + "; " + strings.Join(f.Values, ",") + ")"
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
	kinds := map[*types.TypeName]string{}
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
		kinds[tn] = kind
	}

	// --- Rule 2 (gather): members are top-level vars initialised by New/NextInt.
	// Files are walked in name order — the runtime's package initialisation order
	// — so the NextInt simulation below hands out the same values the runtime
	// counter would (next = max so far + 1, or 0 before any int member exists). ---
	files := append([]*ast.File(nil), pass.Files...)
	sort.Slice(files, func(i, j int) bool {
		return pass.Fset.Position(files[i].Package).Filename < pass.Fset.Position(files[j].Package).Filename
	})
	goodInit := map[*ast.CallExpr]bool{}
	memberVars := map[*types.Var]bool{}
	vals := map[*types.TypeName]*valueState{}
	for _, f := range files {
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
					ce, fnName, arg := newCall(pass, vs.Values[i])
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
					st := vals[tn]
					if st == nil {
						st = &valueState{}
						vals[tn] = st
					}
					st.record(pass, fnName, ce, kinds[tn])
				}
			}
		}
	}

	// Export member sets so switches (and cast sites) in importing packages can
	// be checked.
	localFacts := map[*types.TypeName]*membersFact{}
	for tn := range localEnum {
		names := append([]string(nil), members[tn]...)
		sort.Strings(names)
		f := &membersFact{Names: names, Kind: kinds[tn], Complete: true}
		if st := vals[tn]; st != nil {
			f.Complete = !st.incomplete
			if f.Complete {
				f.Values = append([]string(nil), st.values...)
				sortValues(f.Values, f.Kind)
			}
		}
		localFacts[tn] = f
		pass.ExportObjectFact(tn, f)
	}

	lookupFact := func(tn *types.TypeName) (*membersFact, bool) {
		if f, ok := localFacts[tn]; ok {
			return f, true
		}
		var f membersFact
		if pass.ImportObjectFact(tn, &f) {
			return &f, true
		}
		return nil, false
	}

	lookupMembers := func(tn *types.TypeName) ([]string, bool) {
		f, ok := lookupFact(tn)
		if !ok {
			return nil, false
		}
		return f.Names, true
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
	// and must target an enum type from this same package. New's value argument
	// must also be a compile-time constant, so the whole member set is knowable
	// at analysis time (which the exhaustiveness and, later, cast-site rules
	// depend on). ---
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
		if fnName == "New" && len(ce.Args) > 0 && pass.TypesInfo.Types[ce.Args[0]].Value == nil {
			pass.Reportf(ce.Args[0].Pos(), "enum.New value must be a compile-time constant")
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

	// --- Rule 4: casts require exactly equal value sets. ---
	insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
		ce := n.(*ast.CallExpr)
		fnName, targs := castCall(pass, ce)
		if fnName == "" {
			return
		}
		var fromT, toT types.Type
		switch fnName {
		case "SameValues": // SameValues[A, B]()
			if targs.Len() < 2 {
				return
			}
			fromT, toT = targs.At(0), targs.At(1)
		default: // LookupAs/As/MustAs[To, V, From, PTo]
			if targs.Len() < 3 {
				return
			}
			toT, fromT = targs.At(0), targs.At(2)
		}
		fromN, okFrom := fromT.(*types.Named)
		toN, okTo := toT.(*types.Named)
		if !okFrom || !okTo {
			return
		}
		fromFact, ok := lookupFact(fromN.Obj())
		if !ok {
			return
		}
		toFact, ok := lookupFact(toN.Obj())
		if !ok {
			return
		}
		if fromFact.Kind != toFact.Kind {
			// Only reachable via SameValues — the get/set constraints make a
			// cross-kind LookupAs/As/MustAs a compile error.
			pass.Reportf(ce.Pos(), "%s and %s can never have the same values: one is string-backed, the other int-backed", rel(fromT), rel(toT))
			return
		}
		if !fromFact.Complete || !toFact.Complete {
			return // some member value is not statically known; nothing to compare
		}
		onlyFrom := setDiff(fromFact.Values, toFact.Values)
		onlyTo := setDiff(toFact.Values, fromFact.Values)
		if len(onlyFrom) == 0 && len(onlyTo) == 0 {
			return
		}
		var msg string
		if fnName == "SameValues" {
			msg = "value sets of " + rel(fromT) + " and " + rel(toT) + " differ"
		} else {
			msg = "cannot cast " + rel(fromT) + " to " + rel(toT) + ": value sets differ"
		}
		if len(onlyFrom) > 0 {
			msg += "; only in " + rel(fromT) + ": " + fmtValues(onlyFrom, fromFact.Kind)
		}
		if len(onlyTo) > 0 {
			msg += "; only in " + rel(toT) + ": " + fmtValues(onlyTo, toFact.Kind)
		}
		pass.Reportf(ce.Pos(), "%s", msg)
	})

	return nil, nil
}

// valueState accumulates an enum type's backing values while member decls are
// walked in initialisation order, mirroring the runtime NextInt counter.
type valueState struct {
	values     []string
	incomplete bool // some value is not a compile-time constant
	anyInt     bool
	maxInt     int64
}

func (st *valueState) record(pass *analysis.Pass, fnName string, ce *ast.CallExpr, kind string) {
	switch fnName {
	case "NextInt":
		n := int64(0)
		if st.anyInt {
			n = st.maxInt + 1
		}
		st.addInt(n)
	case "New":
		if len(ce.Args) == 0 {
			st.incomplete = true
			return
		}
		v := pass.TypesInfo.Types[ce.Args[0]].Value
		if v == nil {
			st.incomplete = true
			return
		}
		if kind == "IntEnum" {
			n, ok := constant.Int64Val(constant.ToInt(v))
			if !ok {
				st.incomplete = true
				return
			}
			st.addInt(n)
		} else {
			if v.Kind() != constant.String {
				st.incomplete = true
				return
			}
			st.values = append(st.values, constant.StringVal(v))
		}
	}
}

func (st *valueState) addInt(n int64) {
	st.values = append(st.values, strconv.FormatInt(n, 10))
	if !st.anyInt || n > st.maxInt {
		st.maxInt = n
	}
	st.anyInt = true
}

// sortValues orders a value set canonically: numerically for int enums (the
// values are decimal renderings), lexicographically for string enums.
func sortValues(vals []string, kind string) {
	if kind == "IntEnum" {
		sort.Slice(vals, func(i, j int) bool {
			a, _ := strconv.ParseInt(vals[i], 10, 64)
			b, _ := strconv.ParseInt(vals[j], 10, 64)
			return a < b
		})
		return
	}
	sort.Strings(vals)
}

// setDiff returns the elements of a not present in b, preserving a's order.
func setDiff(a, b []string) []string {
	in := make(map[string]bool, len(b))
	for _, v := range b {
		in[v] = true
	}
	var out []string
	for _, v := range a {
		if !in[v] {
			out = append(out, v)
		}
	}
	return out
}

// fmtValues renders a value list for a diagnostic: quoted for string enums,
// bare decimal for int enums.
func fmtValues(vals []string, kind string) string {
	if kind != "IntEnum" {
		quoted := make([]string, len(vals))
		for i, v := range vals {
			quoted[i] = strconv.Quote(v)
		}
		vals = quoted
	}
	return strings.Join(vals, ", ")
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

// enumFunc resolves ce's callee to a function of the enum package, returning
// the function object and the identifier carrying its instantiation (both nil
// when the call is something else).
func enumFunc(pass *analysis.Pass, ce *ast.CallExpr) (*types.Func, *ast.Ident) {
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
		return nil, nil
	}
	fn, ok := pass.TypesInfo.Uses[id].(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != enumPkgPath {
		return nil, nil
	}
	return fn, id
}

// newCall reports whether e is a call to enum.New or enum.NextInt, returning the
// call expression, the function name, and the first type argument (the enum type).
func newCall(pass *analysis.Pass, e ast.Expr) (*ast.CallExpr, string, types.Type) {
	ce, ok := astutil.Unparen(e).(*ast.CallExpr)
	if !ok {
		return nil, "", nil
	}
	fn, id := enumFunc(pass, ce)
	if fn == nil || (fn.Name() != "New" && fn.Name() != "NextInt") {
		return nil, "", nil
	}
	var arg types.Type
	if inst := pass.TypesInfo.Instances[id]; inst.TypeArgs != nil && inst.TypeArgs.Len() > 0 {
		arg = inst.TypeArgs.At(0)
	}
	return ce, fn.Name(), arg
}

// castCall reports whether ce is a call to one of the cast-family functions
// (enum.LookupAs/As/MustAs/SameValues), returning the function name and its
// full instantiated type-argument list ("" when it is something else).
func castCall(pass *analysis.Pass, ce *ast.CallExpr) (string, *types.TypeList) {
	fn, id := enumFunc(pass, ce)
	if fn == nil {
		return "", nil
	}
	switch fn.Name() {
	case "LookupAs", "As", "MustAs", "SameValues":
	default:
		return "", nil
	}
	inst := pass.TypesInfo.Instances[id]
	if inst.TypeArgs == nil {
		return "", nil
	}
	return fn.Name(), inst.TypeArgs
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
