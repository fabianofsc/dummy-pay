# ADR-0014. Assert with testify/require and compare structs with go-cmp

- **Status:** Accepted
- **Date:** 2026-08-10
- **Supersedes:** —
- **Superseded by:** —

## Context

The project is built test-first, so the ergonomics of writing an assertion are
paid for on every step of every phase, not once.

Two candidates, which are not the same category of tool. `go-cmp` is comparison
only: `cmp.Diff` returns a textual diff and nothing else, with no transitive
dependencies. `testify` is a full assertion library — `require`/`assert`, plus
`mock` and `suite` — bringing three or four transitive modules.

Each has a defect that this codebase activates.

**go-cmp panics on unexported fields.** It does not fail the test; it aborts.
The value objects in spec §2.1 are constructed through validating functions,
which means unexported fields, so the first `cmp.Diff` over a `Payment` would
panic.

**testify's `require.Equal` is wrong for `time.Time`.** It falls through to
`reflect.DeepEqual`, which compares the monotonic reading and the `Location`
pointer. Two instants that `t1.Equal(t2)` considers equal can fail — the classic
case being a timestamp that has been through PostgreSQL, returning without a
monotonic reading and with a different location, compared against the original.
This service is dense in timestamps, and spec §5.1 requires asserting a due time
exactly.

The dependency-count argument deserves less weight than it first appears. Test
dependencies do not enter the binary. The cost is a `go.mod` line and
supply-chain surface, not artefact weight.

## Decision

Use both, with separated roles:

- **`testify/require`** for control flow and simple assertions: `NoError`,
  `ErrorIs`, `True`, `Len`. Never `assert` — a failed assertion should stop the
  test rather than let it continue into a cascade of unrelated failures.
- **`google/go-cmp`** for comparing structs, via `cmp.Diff` reported as
  `(-want +got)`.

**`require.Equal` is never used on a struct containing a timestamp.** That is
the rule this decision exists to make explicit, and it is stated in AGENTS.md.

Domain value objects gain an `Equal` method. This resolves the unexported-field
problem at its source rather than through `cmp.AllowUnexported`, and the method
is useful in production code, so it is not test-only scaffolding.

`testify/mock` is not used. Fakes are written by hand against the ports declared
in spec §8 ([ADR-0003](adr-0003-lean-hexagonal-architecture.md)) — they are few,
and hand-written fakes read better than configured mocks. `testify/suite` is not
used either; `t.Cleanup` covers schema-per-test teardown.

## Consequences

**Positive**

- The boilerplate that dominates a test-first codebase — `if err != nil {
  t.Fatalf(...) }` — collapses to one call, and that saving compounds across
  every step in the plan.
- Struct comparison produces a readable unified diff instead of a dump, which
  matters most for the wide rows in this schema.
- Timestamps compare correctly by construction, because go-cmp honours
  `time.Time`'s own `Equal`.
- Neither library's defect is reachable: unexported fields are handled by the
  `Equal` methods, and timestamps never reach `reflect.DeepEqual`.

**Negative**

- Two assertion idioms appear in the same file, which is a real readability
  cost and looks inconsistent to someone seeing it for the first time.
- The rule against `require.Equal` on timestamped structs is a convention, and
  conventions decay. Left unenforced it becomes folklore, and the failure it
  prevents is the worst kind: a test that passes today and breaks later when the
  value starts coming from the database.
- Two test dependencies rather than one or none.

## Compliance

A CI check fails on `require.Equal` applied to any of the domain aggregate types
(`Payment`, `Delivery`, `Subscription`). This is the enforcement that keeps the
central rule from decaying into folklore.

`assert` is not imported anywhere; only `require`. `testify/mock` and
`testify/suite` are not imported.

## Notes

Considered and rejected: **go-cmp alone with a local `testutil` helper**, which
gives zero transitive dependencies at the price of maintaining a private
mini-assertion library that every contributor has to learn instead of the one
they already know. Also **testify alone**, rejected because the `time.Time`
defect is not a one-time fix but a thing to remember at every timestamp
assertion in a timestamp-heavy service.
