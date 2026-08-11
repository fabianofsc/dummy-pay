# ADR-0002. Accept no cardholder data; select outcomes with scenario tokens

- **Status:** Accepted
- **Date:** 2026-08-10
- **Supersedes:** —
- **Superseded by:** —

## Context

DummyPay simulates card sales. The obvious design accepts a card-shaped payload
— PAN, expiry, CVV, holder name — because that is what a real provider's API
looks like, and realism is the point of a test double.

That instinct is wrong here, and expensively so. A system that *accepts* a PAN
is inside PCI DSS scope even if it stores nothing. The data transits it, which
brings network segmentation, logging controls, access review, and audit
obligations to whatever host it runs on. DummyPay is meant to run on laptops, in
CI runners, in shared development environments, and in demos — exactly the
places where that burden is unacceptable and where controls are weakest.

There is a second problem. Providers express test outcomes through magic card
numbers, and those tables change. An integration suite pinned to a magic PAN
breaks for reasons unrelated to the code under test.

## Decision

The API has no field for a PAN, CVV, expiry date, or cardholder name, and none
will be added.

Payment outcome is selected by an opaque scenario token from a closed set:
`card_approved`, `card_declined`, `card_processing_approved`,
`card_processing_declined`. Request bodies reject unknown fields, so a field
named `card_number` cannot be smuggled through and silently persisted.

Nothing in the service logs a raw request body.

## Consequences

**Positive**

- PCI scope is zero structurally, not by policy. The service cannot leak what it
  has no field to receive.
- It runs anywhere without a compliance conversation, which is what makes it
  usable in CI and on developer machines.
- Outcomes are named, not numbered. `card_processing_declined` says what it does
  and will still say it next year.
- Rejecting unknown fields means a caller who accidentally sends real card data
  gets an error rather than a silent write.

**Negative**

- Client code that formats or validates card fields is not exercised. Teams
  still need their own tests for that layer; DummyPay does not replace them.
- It is not a drop-in substitute for a provider SDK whose client expects card
  input. The integration seam has to sit behind the card-entry step.
- Adding a card field later would not be a feature — it would reverse the
  security posture of the whole project and requires a superseding ADR.

## Compliance

Request decoding rejects unknown fields, with a test asserting that a body
containing `card_number` returns a validation error and creates nothing. The
token set is validated against a closed enum at the HTTP boundary. No log
statement includes the raw request body.

## Notes

Related: [ADR-0006](adr-0006-uuidv7-identifiers.md) covers identifier opacity.
