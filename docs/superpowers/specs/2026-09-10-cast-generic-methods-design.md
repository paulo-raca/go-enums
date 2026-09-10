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

Two type parameters, one of which is inferred. The callsite carries the same
information but reads as what it is — a conversion of a value.

## Verified constraints

Confirmed empirically against a real `go1.27.0` toolchain before this design was
accepted. The design depends on all of these.

1. **Generic methods promote through embedding.** `Suit` embeds
   `StringEnum[Suit]`; `suit.As[ApiSuit]()` resolves on the promoted method.
2. **`PTo` still infers from `*To`.** Only `To` is named at the callsite, so
   the ergonomics match the current function form exactly.
3. **Cross-kind casts remain compile errors.** `suit.TryAs[Color]()` fails with
   `*Color does not satisfy interface{set(string); *Color} (wrong type for
   method set)`. The safety property is preserved by the same mechanism.
4. **`go/types` exposes what the analyzer needs.** For a generic method call,
   `TypesInfo.Instances[sel.Sel].TypeArgs` yields `To` at index 0, and
   `TypesInfo.Types[sel.X].Type` yields the *static type of the receiver
   expression* — which may be `*SqlSuit` rather than `SqlSuit`, hence the
   unwrapping requirement in the enumcheck section below.
5. **The full six-method design compiles and behaves.** A faithful spike of both
   bases over the shared helpers reproduced every current property: round-trip
   cast on string and int enums, zero-casts-to-zero with `ok == true`, miss →
   `(zero, false)`, `As` → `*InvalidValueError[To]` matched by `errors.As`, and
   `MustAs` panicking with that same typed error.
6. **`golang.org/x/tools` v0.45.0 already handles generic methods.** Loading a
   package containing one through `packages.Load` yields zero errors and
   `sig.TypeParams().Len() == 1`. **No dependency bump is required.**
7. **`analysistest`'s GOPATH mode parses generic methods.** A probe analyzer run
   through `analysistest.Run` over testdata containing a generic method reported
   `MapTo typeparams=1`, matching its `// want` comment; a generic type alias
   (a Go 1.24 feature) also typechecked. A negative control — mutating the
   `// want` pattern — failed as expected, confirming the harness was live
   rather than silently skipping. **No testdata restructuring into modules is
   required.**

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

**Addendum (follow-up, same PR):** the asymmetry noted above was subsequently
closed from the other side — `enum.Lookup` is renamed `enum.TryParse`, giving
both families the same Try / plain / Must shape:

| | comma-ok | error | panic |
|---|---|---|---|
| value lookup | `enum.TryParse[T]` | `enum.Parse[T]` | `enum.MustParse[T]` |
| cast | `from.TryAs[To]()` | `from.As[To]()` | `from.MustAs[To]()` |

`TryParse` stays package-level — it is type-level and has no receiver — so the
remaining asymmetry is function-vs-method, which is inherent to the operations
rather than a naming choice. `Valid[T]` keeps its name: it is a bool predicate,
not a member of the trio.

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

**Two** unexported helpers carry all the logic, so nothing is triplicated across
the six methods:

```go
// comma-ok cast
func castTo[To Enum, V any, PTo interface {
	*To
	set(V)
}](val V, index int) (To, bool)

// error-returning cast, wrapping a miss in *InvalidValueError[To]
func castErr[To Enum, V any, PTo interface {
	*To
	set(V)
}](val V, index int) (To, error)
```

`index` is the receiver's raw 1-based field. `index == 0` (the zero value)
returns the zero `To` with `ok == true`, preserving today's "unset travels
across the cast" semantics. Otherwise `castTo` resolves `val` against `To`'s
registry via the existing `resolve[To]`. `castErr` delegates to `castTo`.

`TryAs` → `castTo` and `As` → `castErr` are one-line delegations; `MustAs` is
the three-line `castErr`-then-panic-on-error form, on both bases. Two helpers
rather than one is deliberate — with only `castTo`, the `*InvalidValueError[To]`
construction would be duplicated in `As` and `MustAs` on both bases, four times
over.

This mirrors how `String`, `IsValid`, `IsZero`, `Index` and `Compare` are
already mirrored across the two bases.

*Considered and rejected:* extracting a shared `base[T Enum, V any]` struct that
both bases embed, declaring the trio once. It would eliminate the mirroring, but
it restructures both public types, perturbs `set`/`setIndex` promotion and the
phantom-`T` scheme, and touches far more than casting. Out of scope.

### `get()` is deleted

`StringEnum.get()` (`string.go:87`) and `IntEnum.get()` (`int.go:93`) exist
solely to let the package-level cast functions read a member's backing value
generically; `enum/enum.go` holds their only callers (lines 446, 455, 464, 471,
483). Methods read `e.val` directly, so both methods and the three `get() V`
constraint clauses are removed — from the analyzer's stub package too.

### Doc-comment convention

Canonical prose lives on the `StringEnum` methods; the `IntEnum` methods carry
`// see StringEnum.TryAs` style one-liners, matching the existing convention for
`Index`, `IsZero`, `Compare`, `set`, `get` and `setIndex`. The six methods must
not end up with six copies of the cast documentation.

### Files

**`enum/enum.go`**
- Delete `LookupAs`, `As`, `MustAs` (lines 426-493).
- Add `castTo` and `castErr`.
- `SameValues`, `resolve`, `valueSet`, `symDiff` unchanged.
- Update the casting bullet in the package doc comment; bump the
  `Requires Go 1.24+` line (line 63) to `1.27+`.

**`enum/string.go`**
- Add `TryAs` / `As` / `MustAs` on `StringEnum[T]`, constraint `set(string)`,
  carrying the canonical prose.
- Delete `get()` (line 87) together with its doc comment (lines 84-86), which
  names `LookupAs/As/MustAs` explicitly.

**`enum/int.go`**
- Add `TryAs` / `As` / `MustAs` on `IntEnum[T]`, constraint `set(int)`, with
  `see StringEnum.X` comments.
- Delete `get()` (line 93).

**`enumcheck/enumcheck.go`**
- `enumFunc` (lines 484-506) is the shared AST-unwrapping helper, used by both
  `newCall` (rule 2) and `castCall` (rule 4). It already resolves a promoted
  generic-method call correctly — `fn.Pkg().Path()` is the enum package — so the
  change here is smaller than a rewrite. Take care not to regress rule 2.
- `castCall` (declared at line 529) must return the receiver expression
  alongside the name and type args, and its name switch (line 535) swaps
  `"LookupAs"` → `"TryAs"`. Its doc comment (lines 526-528) names
  `enum.LookupAs/As/MustAs/SameValues` and describes the old return signature;
  rewrite it for the method form.
- Rule 4 (lines 299-359) takes `toT = targs.At(0)` and
  `fromT = TypesInfo.Types[sel.X].Type`, replacing the current
  `targs.At(0)` / `targs.At(2)` indexing.
- **Unwrap the receiver type** before the `fromT.(*types.Named)` assertion at
  lines 319-322: apply `types.Unalias` and dereference `*types.Pointer`.
  A call on an addressable variable (`p := &SqlHearts; p.TryAs[ApiSuit]()`)
  compiles fine and yields a receiver type of `*SqlSuit`; without unwrapping it
  fails the assertion and **silently skips rule 4**. The function form had no
  such hole because it took a value. This is a new blind spot introduced by the
  method form and must be closed.
- A receiver written as the embedded base (`s.StringEnum.TryAs[ApiSuit]()`)
  yields `enum.StringEnum[SqlSuit]`. That is a `*types.Named`, but `lookupFact`
  will miss it, so rule 4 degrades safely to a skip — no false positive. Leave
  it as a skip; recovering `From` from the base's type argument is possible but
  unnecessary, since the shape is pathological.
- The `SameValues` branch is unchanged — it stays a package-level function call
  with two type arguments, and its `sel.X` is the package identifier rather than
  a receiver, so the new receiver return must be ignored on that path.
- Update the stale references to `LookupAs`: the rule-4 description in the file
  doc comment (line 17) and the internal comments at lines 313 and 334.

**`enumcheck/testdata/src/github.com/paulo-raca/go-enums/enum/enum.go`** (stub)
- Delete `LookupAs`, `As`, `MustAs` (lines 23-46) and both `get()` methods
  (lines 8, 13).
- Delete the comment at lines 20-21 — "the same type arguments
  (To=0, V=1, From=2, PTo=3) the analyzer reads". It sits *between* the two
  deletion ranges above, so it survives a literal reading of this list, and it
  documents precisely the indexing scheme this change abandons.
- Add the six methods to the stub's `StringEnum` / `IntEnum`.
- The stub's type parameters are `[T any]`, not `[T Enum]`; keep that — it only
  needs to typecheck, not enforce.

**`enumcheck/testdata/src/c/c.go`, `.../src/d/d.go`**
- Rewrite the cast sites (c.go:71-79, 86-90; d.go:11-13) to method form, keeping
  the `// want` comments attached to the same diagnostics.
- Update the stale `LookupAs` reference in the comment at c.go:96.
- **Add a pointer-receiver case** exercising the unwrapping fix above, with a
  `// want` diagnostic proving rule 4 still fires through a `*T` receiver.

**`README.md`**
- Line 7: `Requires Go 1.24+.` → `1.27+`.
- Lines 185-187: the cast example block → method form.
- Line 190: "Only the target type is named at the call site (`V` and `From` are
  inferred)" — `V` and `From` no longer exist; rewrite for `PTo`.
- Line 196: the cross-kind compile-error example → method form.
- Lines 237, 243: the rule-4 description and example.

**`enumcheck/README.md`**
- Line 83: the rule-4 description naming `enum.LookupAs` / `enum.As` /
  `enum.MustAs`.
- Line 91: the `enum.MustAs[PartialSuit](sqlHearts)` example.

### Toolchain and CI

- `go.mod`: `go 1.24` → `go 1.27`.
- `enumcheck/go.mod`: `go 1.25.0` → `go 1.27`. It is a separate module.
  No `golang.org/x/tools` bump is needed (verified constraint 6).
- `.github/workflows/test.yml`: matrix `['1.24', 'stable']` → `['1.27', 'stable']`.
  Note that this is degenerate while 1.27 *is* stable; it regains meaning at the
  1.28 release. Revisit the existing comment "the enumcheck analyzer tracks a
  recent x/tools, so it requires the current Go; exercise it only on the stable
  job" — both jobs now require 1.27, so that gating rationale no longer holds and
  the enumcheck steps can drop their `if: matrix.go == 'stable'` condition.

## Testing

**`enum/enum_test.go`** — existing cast tests port to method form, keeping the
same assertions:
- round-trip cast between parallel string enums and parallel int enums
- zero value casts to zero value, `ok == true`, `IsZero()` holds on the result
- a value with no counterpart in `To` yields `(zero, false)` from `TryAs`
- `As` returns `*InvalidValueError[To]`, matched via `errors.As`
- `MustAs` panics with the same typed error
- **new:** the cast called through a pointer / addressable variable, confirming
  the method set works from `*T` as well as `T`

**`enumcheck`** — `analysistest` runs unchanged once testdata is ported; the
expected diagnostics are identical. The new pointer-receiver testdata case is
the regression test for the unwrapping fix.

**Cross-kind rejection** stays a compile-time property. It is covered by the
shape of the stub and bases rather than a runtime test, exactly as today.

## Migration

Callers rewrite three call forms; the mapping is mechanical and the table above
is the whole of it. The compiler catches every stale callsite, since the
package-level functions are removed rather than deprecated. The repository dates
from 2026-05-29 and the cast API specifically landed in PR #8 on 2026-07-06, so
external exposure to the removed functions is expected to be minimal.
