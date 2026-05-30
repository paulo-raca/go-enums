# go-enums

Generic, closed-set, value-backed enums for Go — without the per-type
boilerplate (`String`, `MarshalText`/`UnmarshalText`, JSON, validation,
listing).

Requires Go 1.24+.

```go
import "github.com/paulo-raca/go-enums/enum"
```

A single constructor, `enum.New`, serves both bases: pass a string for a
`StringEnum` or an int for an `IntEnum`. The value type is inferred, and a
mismatch (`enum.New[Suit](5)`) is a compile-time error.

## String enums

Embed `enum.StringEnum[Self]`; declare members with `enum.New`. The string
value *is* the name.

```go
type Suit struct{ enum.StringEnum[Suit] }

var (
	Hearts   = enum.New[Suit]("hearts")
	Diamonds = enum.New[Suit]("diamonds")
	Clubs    = enum.New[Suit]("clubs")
	Spades   = enum.New[Suit]("spades")
)
```

`json.Marshal(Hearts)` → `"hearts"` (also works as a JSON map key).

## Integer enums

Embed `enum.IntEnum[Self]`. Use `enum.NextInt` for iota-like auto-increment, or
`enum.New` with an explicit value (which the auto-increment counter continues
from). Only the integer is stored; `String()` is its decimal form.

```go
type Color struct{ enum.IntEnum[Color] }

var (
	Red   = enum.NextInt[Color]() // 0
	Green = enum.NextInt[Color]() // 1
	Blue  = enum.NextInt[Color]() // 2
)

type Level struct{ enum.IntEnum[Level] }

var (
	Low  = enum.New[Level](10) // explicit start
	Mid  = enum.NextInt[Level]()  // 11
	High = enum.NextInt[Level]()  // 12
)
```

`json.Marshal(Green)` → `1` (a bare number).

## What you get for free, per type

- `String()` — `fmt.Stringer`; the raw backing value via `StringEnum.String()` /
  `IntEnum.Int()`
- `MarshalText` / `UnmarshalText` — `encoding.Text{Marshaler,Unmarshaler}`
- JSON: `StringEnum` as a string, `IntEnum` as a number (and `null` is a no-op)
- `database/sql`: `driver.Valuer` + `sql.Scanner` (`StringEnum` as text, `IntEnum`
  as `int64`); use a `*T` pointer for a nullable column
- typed `*enum.InvalidValueError[T]` (bad input) and `*enum.ZeroMarshalError[T]`
  (marshalling/persisting the zero value) errors, both matchable with `errors.As`
- `enum.Values[T]()` (in registration order)
- four flavors of value lookup: `enum.Valid[T]` → `bool`, `enum.Lookup[T]` →
  `(T, bool)`, `enum.Parse[T]` → `(T, error)`, `enum.MustParse[T]` → `T` (panics)
- `member.IsValid()` — is this a real member, or the zero value? (lock-free)
- `member.Position()` — 0-based registration order (`-1` for the zero value)
- `a.Compare(b)` — order members by registration position. Go has no operator
  overloading, so `a < b` is `a.Compare(b) < 0` (likewise `<=`, `>`, `>=`)

Enums are **sortable by insertion order**: `Values[T]()` already returns members
in the order they were declared, and `Compare` lets you sort a mixed slice back
into that order with `slices.SortFunc(xs, MyEnum.Compare)`.

## Closed by construction

The backing field and its setter are unexported, so `enum.New` (and the
iota-like `enum.NextInt`) are the only way to mint a member. Any package may
declare enum types and call them, but cannot forge arbitrary values — that's a
compile-time error. The zero value of an enum is constructible but never
registered, so `Valid` reports it `false`. It also stays distinct even from a
member backed by `""` or `0` — i.e. `MyEnum{} != enum.New[MyEnum](0)` — so you
can use `MyEnum{}` as an "unset" sentinel (detect it with `== MyEnum{}` or
`Valid`) and still have a real member at `0`/`""`. The zero value renders as
`<invalid Suit>` (the type name) from `String()` and is refused by the marshallers (its `""`/`0`
output wouldn't round-trip), so an unset enum field surfaces as a marshal error
rather than silently corrupt data — use `json:",omitzero"` or a `*Suit` pointer
if "unset" should be serializable.

Registering the same value twice for a type — a copy-pasted member, or two
`IntEnum` members sharing a value — **panics** at init time rather than passing
silently. Because of this, `New` is meant for package-level `var` blocks (which
run once); don't call it for the same value from code that can run more than
once.

## Validating input

```go
s, ok := enum.Lookup[Suit](untrusted) // (T, bool)
if !ok {
	// reject
}

s, err := enum.Parse[Suit](untrusted)    // (T, error): *InvalidValueError, composes with %w
if err != nil {
	return fmt.Errorf("bad suit: %w", err)
}

s = enum.MustParse[Suit](trustedConst)   // panics on miss — for values you know are members

var got Suit
err = json.Unmarshal(data, &got)
var invalid *enum.InvalidValueError[Suit]
if errors.As(err, &invalid) {
	// invalid.Value holds the offending input
}
```

## Linting with `enumcheck`

Because members are package-level `var`s rather than `const`s, the stock
`exhaustive` linter can't check `switch`es over these enums. The optional
[`enumcheck`](enumcheck) analyzer does — and it also enforces the invariants that
keep the member set statically knowable in the first place.

**What it checks:**

1. **Enum shape** — a type embedding `enum.StringEnum`/`enum.IntEnum` must embed
   exactly that one base and nothing else, parameterised by itself
   (`type Suit struct{ enum.StringEnum[Suit] }`). Extra fields, a non-embedded
   base, or a mismatched `Self` (`enum.StringEnum[Other]`) are flagged.
2. **Member declaration** — `enum.New` / `enum.NextInt` may appear only as the
   direct initialiser of a package-level `var`; member vars may not be
   reassigned; and members must be declared in the enum type's own package
   (so the set is complete and statically enumerable).
3. **Switch exhaustiveness** — in a `switch` over an enum type, every case must
   name a member of that enum, and either all members are covered or a `default`
   clause is present. Works across packages (member sets travel via analysis
   facts).

```go
switch s { // enumcheck: non-exhaustive switch on Suit: missing Spades
case Hearts:
case Diamonds:
}
```

**Standalone, or through `go vet`:**

```sh
go install github.com/paulo-raca/go-enums/enumcheck/cmd/enumcheck@latest
enumcheck ./...
go vet -vettool=$(which enumcheck) ./...
```

**With golangci-lint** (the [module plugin system](https://golangci-lint.run/plugins/module-plugins/)) — add a `.custom-gcl.yml` at your repo root:

```yaml
version: v2.1.0 # match your installed golangci-lint version
plugins:
  - module: github.com/paulo-raca/go-enums/enumcheck
    import: github.com/paulo-raca/go-enums/enumcheck
    version: latest
```

Build a custom binary that bundles the plugin, then run it:

```sh
golangci-lint custom   # produces ./custom-gcl
./custom-gcl run
```

…and enable it in `.golangci.yml`:

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

`enumcheck` lives in its own module, so it adds **no dependencies** to the `enum`
package itself. See [enumcheck/README.md](enumcheck/README.md) for details and
limitations.
