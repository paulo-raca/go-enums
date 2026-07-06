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
   reassigned; and members must be declared in the enum type's own package
   (so the set is complete and statically enumerable).
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

4. **Cast value sets.** A call to `enum.LookupAs` / `enum.As` / `enum.MustAs`
   (or an `enum.SameValues` assertion) between two enum types whose statically
   known backing value sets are not exactly equal is flagged, naming the values
   present on only one side. Works across packages via analysis facts. If a
   member is constructed with a non-constant argument, the check is skipped for
   that type (its value set can't be computed at analysis time).

```go
enum.MustAs[PartialSuit](sqlHearts) // enumcheck: cannot cast SqlSuit to PartialSuit: value sets differ; only in SqlSuit: "spades"
```

## Limitations

- Members must be created in the type's defining package (rule 2); members
  registered elsewhere at runtime are invisible to static analysis (and rejected).
- Re-assignment is detected directly (`Hearts = …`); mutation via a taken address
  (`p := &Hearts; *p = …`) is not yet flagged.
