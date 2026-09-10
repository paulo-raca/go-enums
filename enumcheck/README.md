# enumcheck

A [`go/analysis`](https://pkg.go.dev/golang.org/x/tools/go/analysis) linter for
[`github.com/paulo-raca/go-enums/enum`](../enum). Because the library's members
are package-level `var`s (not `const`s), the standard `exhaustive` linter can't
see them — `enumcheck` fills that gap, plus it enforces the patterns that make
the member set statically knowable in the first place.

It lives in its own module so the core `enum` package stays dependency-free;
`golang.org/x/tools` is pulled in only by users who install the linter.

## Install & run

```sh
go install github.com/paulo-raca/go-enums/enumcheck/cmd/enumcheck@latest

enumcheck ./...
# or through go vet:
go vet -vettool=$(which enumcheck) ./...
```

### With golangci-lint

This module registers a [module-plugin](https://golangci-lint.run/plugins/module-plugins/)
entry point in the `github.com/paulo-raca/go-enums/enumcheck` package. Add a
`.custom-gcl.yml`:

```yaml
version: v2.1.0 # match your installed golangci-lint version
plugins:
  - module: github.com/paulo-raca/go-enums/enumcheck
    import: github.com/paulo-raca/go-enums/enumcheck
    version: latest
```

then `golangci-lint custom` (builds `./custom-gcl`) and enable it in
`.golangci.yml`:

```yaml
version: "2"
linters:
  enable:
    - enumcheck
  settings:
    custom:
      enumcheck:
        type: module
        description: go-enums invariants and switch exhaustiveness
```

## What it checks

1. **Enum shape.** A type that embeds `enum.StringEnum`/`enum.IntEnum` must embed
   exactly that one base and nothing else, parameterised by itself:
   `type Suit struct{ enum.StringEnum[Suit] }`. Extra fields or a mismatched
   `Self` are reported.
2. **Member declaration.** `enum.New` / `enum.NextInt` may appear only as the
   direct initialiser of a package-level `var`; member vars may not be
   reassigned; members must be declared in the enum type's own package; and
   `enum.New`'s value argument must be a compile-time constant (so the whole
   set is knowable at analysis time).

```go
var opaque = "runtime-decided"
var Bad = enum.New[Suit](opaque) // enumcheck: enum.New value must be a compile-time constant
```
3. **Switch exhaustiveness.** In a `switch` over an enum type, every case must
   name a member of that enum, and either all members are covered or a `default`
   clause is present. Works across packages via analysis facts.

```go
func describe(s Suit) string {
	switch s { // enumcheck: non-exhaustive switch on Suit: missing Spades
	case Hearts:
		return "h"
	case Diamonds:
		return "d"
	}
	return ""
}
```

4. **Cast value sets.** A cast (`TryAs` / `As` / `MustAs`) is flagged when the
   source enum has statically known backing values the target lacks — those
   members could not survive the cast. Values present only in the *target* are
   fine: a cast is total as long as the source's set is a subset of the
   target's, so widening (`b.As[A]()` where `B ⊂ A`) is not flagged while
   narrowing (`a.As[B]()`) is. An `enum.SameValues` assertion is stricter and is
   flagged unless the two sets are exactly equal. Works across packages via
   analysis facts. If a member is constructed with a non-constant argument, the
   check is skipped for that type (its value set can't be computed at analysis
   time).

```go
sqlHearts.MustAs[PartialSuit]()     // enumcheck: cannot cast SqlSuit to PartialSuit: missing in PartialSuit: "spades"
partialHearts.MustAs[SqlSuit]()     // ok: PartialSuit ⊂ SqlSuit, the cast is total
```

## Limitations

- Members must be created in the type's defining package (rule 2); members
  registered elsewhere at runtime are invisible to static analysis (and rejected).
- Re-assignment is detected directly (`Hearts = …`); mutation via a taken address
  (`p := &Hearts; *p = …`) is not yet flagged.
- Rule 4 reads the source enum from the cast's receiver, so a receiver whose
  static type isn't the enum itself is skipped rather than flagged: an outer
  struct that embeds it (`type wrap struct{ SqlSuit }`), the embedded base
  (`x.StringEnum.As[To]()`), or a method value (`f := x.As[To]`). Pointers and
  aliases (`p := &Hearts; p.As[To]()`) are resolved. Skips are missed warnings,
  never false ones.
