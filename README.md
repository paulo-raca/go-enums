# go-enums

Generic, closed-set, value-backed enums for Go — without the per-type
boilerplate (`String`, `MarshalText`/`UnmarshalText`, JSON, validation,
listing).

Requires Go 1.22+ (`reflect.TypeFor`).

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

- `String()` — `fmt.Stringer`
- `MarshalText` / `UnmarshalText` — `encoding.Text{Marshaler,Unmarshaler}`
- JSON: `StringEnum` as a string, `IntEnum` as a number
- typed `*enum.InvalidValueError[T]` (bad input) and `*enum.ZeroMarshalError[T]`
  (marshalling the zero value) errors, both matchable with `errors.As`
- `enum.Values[T]()`, `enum.Valid[T](v)`, `enum.FromValue[T](v)`

## Closed by construction

The backing field and its setter are unexported, so `enum.New` (and the
iota-like `enum.NextInt`) are the only way to mint a member. Any package may
declare enum types and call them, but cannot forge arbitrary values — that's a
compile-time error. The zero value of an enum is constructible but never
registered, so `Valid` reports it `false`. It also stays distinct even from a
member backed by `""` or `0` — i.e. `MyEnum{} != enum.New[MyEnum](0)` — so you
can use `MyEnum{}` as an "unset" sentinel (detect it with `== MyEnum{}` or
`Valid`) and still have a real member at `0`/`""`. The zero value renders as
`<invalid>` from `String()` and is refused by the marshallers (its `""`/`0`
output wouldn't round-trip), so an unset enum field surfaces as a marshal error
rather than silently corrupt data — use `json:",omitzero"` or a pointer if
"unset" should be serializable.

Registering the same value twice for a type — a copy-pasted member, or two
`IntEnum` members sharing a value — **panics** at init time rather than passing
silently. Because of this, `New` is meant for package-level `var` blocks (which
run once); don't call it for the same value from code that can run more than
once.

## Validating input

```go
s, ok := enum.FromValue[Suit](untrusted)
if !ok {
	// reject
}

var got Suit
err := json.Unmarshal(data, &got)
var invalid *enum.InvalidValueError[Suit]
if errors.As(err, &invalid) {
	// invalid.Value holds the offending input
}
```
