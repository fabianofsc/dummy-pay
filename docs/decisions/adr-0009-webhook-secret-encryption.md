# ADR-0009. Encrypt webhook secrets with AES-256-GCM under a separate environment key

- **Status:** Accepted
- **Date:** 2026-08-10
- **Supersedes:** —
- **Superseded by:** —

## Context

Registering a webhook subscription returns a secret, shown exactly once. The
service uses that secret to compute the `X-Webhook-Signature` HMAC on every
delivery, and the consumer uses it to verify.

That rules out the usual answer for stored secrets. The service must recover the
original bytes at delivery time, so hashing is not available — the storage has
to be reversible.

Storing it in plaintext means a database dump hands over the ability to forge
signed events that consumers will accept as genuine. Since the entire point of
the signature is to let a consumer trust the payload, plaintext storage would
make the feature decorative.

## Decision

Encrypt the secret with AES-256-GCM before storing it. The key comes from
`DUMMYPAY_WEBHOOK_SECRET_ENC_KEY`, base64-encoded 32 bytes, a variable distinct
from the one holding the database DSN. A fresh random nonce per record is stored
alongside the ciphertext.

The plaintext secret appears in the creation response and nowhere else — not in
any later response, not in logs.

**Why AEAD rather than plain AES-CBC.** GCM authenticates the ciphertext, so
tampering is detected rather than producing a silently wrong key.

**Why a separate variable.** So that a leaked database URL is not by itself
sufficient to compromise signing. If both came from the same secret, the
separation would be nominal.

**Why not a KMS or a secret manager.** Both are external systems, excluded by
the project's independence constraint.

## Consequences

**Positive**

- A database dump alone does not allow forging events.
- Standard library primitives only. No hand-rolled cryptography.
- The separation between database access and signing capability is structural,
  not a policy someone has to follow.

**Negative**

- Losing the key makes every existing subscription unusable. The documented
  recovery is to register a new subscription and update the consumer — there is
  no way back.
- **No key rotation in V1.** Rotating would require a key identifier column and
  a re-encryption path. Adding it later is a superseding decision, and the
  absence should not be mistaken for an oversight.
- The key lives in the environment of the same process that holds the database
  connection, so an attacker with process memory access still gets both. This
  defends against dump-and-exfiltrate, not against full host compromise.

## Compliance

- A test asserts the stored column never equals the plaintext secret.
- A test asserts the secret appears in the creation response and in no other
  response body.
- Startup fails loudly when the key is missing or is not exactly 32 bytes after
  decoding, rather than falling back to a default.

## Notes

Related: [ADR-0010](adr-0010-configuration-from-environment.md) for how the key
reaches the process.
