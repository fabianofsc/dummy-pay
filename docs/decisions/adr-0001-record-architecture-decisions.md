# ADR-0001. Record architecture decisions

- **Status:** Accepted
- **Date:** 2026-08-10
- **Supersedes:** —
- **Superseded by:** —

## Context

DummyPay is a new service with a deliberately small scope. Most of its value
comes from what it refuses to do: it accepts no card data, exposes one payment
operation, and supports one account. Without a written record, a refusal is
indistinguishable from an oversight, and the next contributor "fixes" it.

Several stack choices in this project are defensible either way — UUIDv7 versus
ULID, chi versus the standard library, an in-process worker versus a separate
binary. Decisions made without recorded justification get re-argued every time
someone new reads the code. That is the Groundhog Day anti-pattern, and it is
expensive precisely because the arguments are close.

## Decision

Every architecturally significant decision is recorded as a numbered Markdown
file in `docs/decisions/`, using the sections Context, Decision, Consequences,
Compliance, and Notes.

A decision is architecturally significant, following Nygard's test, when it
affects structure, architecture characteristics, dependencies, interfaces, or
construction techniques. Routine implementation choices are not recorded.

Each ADR states both a technical justification and the product reason behind it.
A decision justified only on technical grounds will be reopened by anyone who
weighs the technical factors differently.

Superseded ADRs are never deleted or edited into agreement with the present.
They are marked `Superseded by ADR-NNNN`, and the replacement records why the
situation changed. The chain is the history.

## Consequences

**Positive**

- One system of record. "Why is it like this?" always has an address.
- The supersession chain answers "why not X?" with evidence rather than opinion.
- New contributors can read `docs/decisions/` and understand the shape of the
  system before reading any code.

**Negative**

- Writing overhead on every significant decision.
- ADRs can drift from the code. An ADR that no longer describes reality is worse
  than no ADR, because it is trusted.

## Compliance

Files under `docs/decisions/` are append-only in practice: a change of direction
is a new ADR that marks the previous one superseded, never an edit that rewrites
the original decision. The README links to the index.

## Notes

Format follows Michael Nygard's ADR structure as presented in *Fundamentals of
Software Architecture* (Richards & Ford), chapter 19.
