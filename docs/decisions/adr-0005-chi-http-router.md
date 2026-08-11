# ADR-0005. Use chi for HTTP routing

- **Status:** Accepted
- **Date:** 2026-08-10
- **Supersedes:** —
- **Superseded by:** —

## Context

V1 exposes three endpoints, one of them with a path parameter
(`/v1/webhook-deliveries/{delivery_id}/retry`). Cross-cutting behaviour is
modest but real: HTTP Basic authentication on `/v1` and *not* on a health
endpoint, a request ID, structured request logging, and panic recovery.

The candidates were the standard library's `net/http` (which gained method and
wildcard routing in Go 1.22), chi, Echo or Gin, and Fiber.

The deciding property is not routing performance — at three endpoints that is
noise. It is **the type of a handler**, because that type determines whether the
HTTP adapter stays a thin translation layer or becomes a framework-shaped part
of the system.

## Decision

chi v5. Handlers remain `http.HandlerFunc`; middleware remains
`func(http.Handler) http.Handler`.

**Why not Echo or Gin.** Both introduce a proprietary context type that appears
in the signature of every handler. That type would spread through the HTTP
adapter and press against the boundary set in
[ADR-0003](adr-0003-lean-hexagonal-architecture.md). Testing goes through the
framework's harness instead of `httptest`, and leaving means rewriting every
handler.

**Why not Fiber.** It runs on fasthttp and is not `net/http`-compatible at all.
`httptest` does not apply, and no standard middleware composes. For a service
whose purpose is to be a trustworthy test target, giving up the standard testing
tools is the wrong trade.

**Why not the standard library alone.** Viable, and genuinely close. It loses on
one specific thing: route groups. Applying authentication to a subset of routes
means wrapping each handler individually, and the failure mode of forgetting one
is an unauthenticated endpoint in a payments API. chi makes the group the unit,
so the mistake is structurally harder to make.

**Why chi is cheap to reverse.** chi is a superset of standard library types
rather than an alternative to them. If it were abandoned tomorrow, migration
means rewriting the routing file; the handlers do not change.

## Consequences

**Positive**

- `httptest.NewRequest` and `ResponseRecorder` work directly, with no adapter.
- Any `func(http.Handler) http.Handler` middleware composes, including ones we
  write ourselves.
- Authentication is attached to a group, so adding a route to `/v1` inherits it
  rather than requiring the author to remember it.
- Exit cost is one file.

**Negative**

- A dependency for something the standard library nearly does.
- `chi/middleware` is a second, softer dependency. We use only request ID, real
  IP, and recoverer from it, and write the rest — the more of that package we
  adopt, the more of the exit cost returns.

## Compliance

A test asserts that an unauthenticated request to every route under `/v1`
returns 401. This catches the specific failure this decision exists to prevent:
a route registered outside the authenticated group. The dependency test from
[ADR-0003](adr-0003-lean-hexagonal-architecture.md) asserts `internal/payment`
does not import chi.

## Notes

Reconsider if the surface grows past roughly a dozen endpoints with varied
middleware needs, or shrinks to a point where the standard library's lack of
groups stops mattering.
