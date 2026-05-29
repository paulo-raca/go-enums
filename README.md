# go-enums

Generic, closed-set, value-backed enums for Go — without the per-type
boilerplate (`String`, `MarshalText`/`UnmarshalText`, JSON, validation,
listing).

Requires Go 1.22+ (`reflect.TypeFor`).

```go
import "github.com/paulo-raca/go-enums/enum"
```

## String enums

Embed `enum.StringEnum[Self]`; declare members with `enum.NewString`. The
string value *is* the name.

```go
type AIStopReason struct{ enum.StringEnum[AIStopReason] }

var (
	AIStopReasonEndTurn   = enum.NewString[AIStopReason]("end_turn")
	AIStopReasonMaxTokens = enum.NewString[AIStopReason]("max_tokens")
	AIStopReasonToolUse   = enum.NewString[AIStopReason]("tool_use")
)
```

`json.Marshal(AIStopReasonEndTurn)` → `"end_turn"` (also works as a JSON map key).

## Integer enums

Embed `enum.IntEnum[Self]`. Use `enum.NextInt` for iota-like auto-increment, or
`enum.NewInt` for explicit values (which the auto-increment counter continues
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
	Low  = enum.NewInt[Level](10) // explicit start
	Mid  = enum.NextInt[Level]()  // 11
	High = enum.NextInt[Level]()  // 12
)
```

`json.Marshal(Green)` → `1` (a bare number).

## What you get for free, per type

- `String()` — `fmt.Stringer`
- `MarshalText` / `UnmarshalText` — `encoding.Text{Marshaler,Unmarshaler}`
- JSON: `StringEnum` as a string, `IntEnum` as a number
- a typed `*enum.InvalidError[T]` on bad input (use with `errors.As`)
- `enum.Values[T]()`, `enum.Valid[T](v)`, `enum.FromString[T](s)`, `enum.FromInt[T](n)`

## Closed by construction

The backing field and its setter are unexported, so the `New*` constructors are
the only way to mint a member. Any package may declare enum types and call them,
but cannot forge arbitrary values — that's a compile-time error. The zero value
of an enum is constructible but never registered, so `Valid` reports it `false`;
guard with `Valid` or treat the zero value as an explicit sentinel.

## Validating input

```go
r, ok := enum.FromString[AIStopReason](untrusted)
if !ok {
	// reject
}

var got AIStopReason
err := json.Unmarshal(data, &got)
var invalid *enum.InvalidError[AIStopReason]
if errors.As(err, &invalid) {
	// invalid.Value holds the offending input
}
```
