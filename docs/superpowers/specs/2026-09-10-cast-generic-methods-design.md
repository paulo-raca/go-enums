# Cast trio as Go 1.27 generic methods

**Date:** 2026-09-10
**Status:** Approved (design)

## Summary

Move the cross-enum cast operations from package-level generic functions to
generic methods on the two enum bases, using the generic-methods feature
introduced in Go 1.27:

```go
// before
api, err := enum.As[ApiSuit](sqlSuit)

// after
api, err := sqlSuit.As[ApiSuit]()
```

This is a **breaking change**, taken deliberately as a clean replacement rather
than an additive one. It raises the module floor to Go 1.27.

## Motivation

The cast functions carry the most awkward declarations in the package. `As` is
currently:

```go
func As[To Enum, V any, From interface {
	Enum
	get() V
}, PTo interface {
	*To
	set(V)
}](from From) (To, error)
```

Four type parameters, of which three exist only to recover information the
receiver already has. As a method, `V` is fixed by the base and `From` is the
receiver, leaving:

```go
func (e StringEnum[T]) As[To Enum, PTo interface{ *To; set(string) }]() (To, error)
```

Two type parameters, one of which is inferred. The callsite is unchanged in
information content but reads as what it is — a conversion of a value.

## Verified constraints

The following were confirmed empirically against a real `go1.27.0` toolchain
before this design was accepted. They are recorded here because the design
depends on all four.

1. **Generic methods promote through embedding.** `Suit` embeds
   `StringEnum[Suit]`; `suit.As[ApiSuit]()` resolves on the promoted method.
2. **`PTo` still infers from `*To`.** Only `To` is named at the callsite, so
   the ergonomics match the current function form exactly.
3. **Cross-kind casts remain compile errors.** `suit.TryAs[Color]()` fails with
   `*Color does not satisfy interface{set(string); *Color} (wrong type for
   method set)`. The safety property is preserved by the same mechanism.
4. **`go/types` exposes what the analyzer needs.** For a generic method call,
   `TypesInfo.Instances[sel.Sel].TypeArgs` yields `To` at index 0, and
   `TypesInfo.Types[sel.X].Type` yields the receiver (`From`).

### Known limitation (accepted)

Go 1.27 forbids type parameters on interface methods
(`interface method must have no type parameters`), so the `Enum` constraint
cannot declare the cast methods, and generic code parameterised over `T Enum`
cannot call them.

**This costs nothing.** The current function form requires
`From interface{ Enum; get() V }`, and `get()` is unexported, so no external
package can name that constraint today. The capability being given up was never
reachable outside `package enum`.

## Naming

The comma-ok form is renamed `LookupAs` → `TryAs`. `As` and `MustAs` keep their
names.

`Lookup` was chosen as a package-level verb and does not read well on a
receiver. This deliberately breaks symmetry with the value-lookup trio
(`enum.Lookup` / `enum.Parse` / `enum.MustParse`), which stays package-level
because those operations are type-level and have no receiver.

| Before                        | After                    |
|-------------------------------|--------------------------|
| `enum.LookupAs[To](from)`     | `from.TryAs[To]()`       |
| `enum.As[To](from)`           | `from.As[To]()`          |
| `enum.MustAs[To](from)`       | `from.MustAs[To]()`      |
| `enum.SameValues[A, B]()`     | *(unchanged)*            |

## Scope

Only the cast trio moves. Every other package-level function is **type-level** —
it takes no member instance — and stays exactly as it is: `New`, `NextInt`,
`Values`, `Valid`, `Lookup`, `Parse`, `MustParse`, `ValuesWithTag`,
`ValuesWithAnyTags`, `ValuesWithAllTags`, `SameValues`. Rendering those as
methods would require calling them on a throwaway zero value
(`Suit{}.Values()`), which is worse than the status quo.

## Design

### Structure

The trio must be declared on both `StringEnum[T]` (`set(string)`) and
`IntEnum[T]` (`set(int)`) — six methods, because a single declaration cannot
abstract over the backing kind.

All six are one-line delegations to a shared unexported helper, so the logic
exists once:

```go
func castTo[To Enum, V any, PTo interface {
	*To
	set(V)
}](val V, index int) (To, bool)
```

`index` is the receiver's raw 1-based field. `index == 0` (the zero value)
returns the zero `To` with `ok == true`, preserving today's "unset travels
across the cast" semantics. Otherwise it resolves `val` against `To`'s registry
via the existing `resolve[To]`.

This mirrors how `String`, `IsValid`, `IsZero`, `Index` and `Compare` are
already mirrored across the two bases.

*Considered and rejected:* extracting a shared `base[T Enum, V any]` struct that
both bases embed, declaring the trio once. It would eliminate the mirroring, but
it restructures both public types, perturbs `set`/`setIndex` promotion and the
phantom-`T` scheme, and touches far more than casting. Out of scope.

### `get()` is deleted

`StringEnum.get()` and `IntEnum.get()` exist solely to let the package-level
cast functions read a member's backing value generically; `enum.go` holds their
only callers. Methods read `e.val` directly, so both methods and the three
`get() V` constraint clauses are removed — from the analyzer's stub package too.

### Files

**`enum/enum.go`**
- Delete `LookupAs`, `As`, `MustAs`.
- Add `castTo`.
- `SameValues`, `resolve`, `valueSet`, `symDiff` unchanged.
- Update the casting bullet in the package doc comment; bump the
  `Requires Go 1.24+` line to `1.27+`.

**`enum/string.go`**
- Add `TryAs` / `As` / `MustAs` on `StringEnum[T]`, constraint `set(string)`.
- Delete `get()`.

**`enum/int.go`**
- Add `TryAs` / `As` / `MustAs` on `IntEnum[T]`, constraint `set(int)`.
- Delete `get()`.

**`enumcheck/enumcheck.go`**
- `castCall` learns the method shape: unwrap `IndexExpr` / `IndexListExpr` to
  reach the `SelectorExpr`, resolve `TypesInfo.Uses[sel.Sel]` to a `*types.Func`,
  and confirm `fn.Pkg().Path() == enumPkgPath`. It must return the receiver
  expression alongside the name and type args.
- Rule 4 takes `toT = targs.At(0)` and `fromT = TypesInfo.Types[sel.X].Type`,
  replacing the current `targs.At(0)` / `targs.At(2)` indexing.
- The `SameValues` branch is unchanged — it stays a package-level function call
  with two type arguments.
- Diagnostic wording is unchanged, so existing `// want` regexes stay valid.
- Update the rule-4 description in the file's doc comment (line ~17).

**`enumcheck/testdata/src/github.com/paulo-raca/go-enums/enum/enum.go`** (stub)
- Delete `LookupAs`, `As`, `MustAs`, and both `get()` methods.
- Add the six methods to the stub's `StringEnum` / `IntEnum`.
- The stub's type parameters are `[T any]`, not `[T Enum]`; keep that — it only
  needs to typecheck, not enforce.

**`enumcheck/testdata/src/c/c.go`, `.../src/d/d.go`**
- Rewrite the cast sites (c.go:71-79, 86-90; d.go:11-13) to method form,
  keeping the `// want` comments attached to the same diagnostics.

**`README.md`** — cast section and any cast examples.

### Toolchain and CI

- `go.mod`: `go 1.24` → `go 1.27`.
- `enumcheck/go.mod`: `go 1.25.0` → `go 1.27`. It is a separate module, and its
  testdata (compiled by `analysistest` in GOPATH mode) contains generic methods.
- `.github/workflows/test.yml`: matrix `['1.24', 'stable']` → `['1.27', 'stable']`.
- **Verify** `golang.org/x/tools v0.45.0` parses generic-method syntax. The
  analyzer depends on `go/ast` and `go/types` through x/tools; if v0.45.0
  predates Go 1.27 support, bump it. This is a prerequisite, not a follow-up —
  the analyzer cannot be ported until it can parse the syntax.

## Testing

**`enum/enum_test.go`** — existing cast tests port to method form, keeping the
same assertions:
- round-trip cast between parallel string enums and parallel int enums
- zero value casts to zero value, `ok == true`, `IsZero()` holds on the result
- a value with no counterpart in `To` yields `(zero, false)` from `TryAs`
- `As` returns `*InvalidValueError[To]`, matched via `errors.As`
- `MustAs` panics with the same typed error

**`enumcheck`** — `analysistest` runs unchanged once testdata is ported; the
expected diagnostics are identical.

**Cross-kind rejection** stays a compile-time property. It is covered by the
shape of the stub and bases rather than a runtime test, exactly as today.

## Migration

Callers rewrite three call forms; the mapping is mechanical and the table above
is the whole of it. The compiler catches every stale callsite, since the
package-level functions are removed rather than deprecated. The library has been
public for roughly two months and the cast API landed in PR #8 (2026-07-06), so
external exposure is expected to be minimal.
