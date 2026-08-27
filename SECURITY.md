# Security policy

This library is how a signing service publishes its material signing events. Downstream, those
events become a hash-chained trail whose job is to let someone later **find and trust** the
cryptographic evidence for a signature. The trail is only as good as what this emitter puts into
it, and it is deliberately lean: it carries references to evidence, never the evidence itself, and
it must never carry certificates, digests, revocation data, archive material, validation blobs or
document bytes.

So it has two opposite failure modes, and both matter: an event that does not reach the trail (or
reaches it wrong), and an event that carries more than it should.

Please report security problems privately. Do not open a public issue, pull request or discussion
for anything that could be exploited before a fix exists.

## How to report

Use **[private vulnerability reporting](https://github.com/gmb-lib/go-eidas-audit/security/advisories/new)**
on this repository. The report stays visible only to you and the maintainers until an advisory is
published, and it gives us one place to discuss and co-ordinate a fix with you.

Please include, as far as you can establish it:

- what the problem is, and what an attacker or an unintended reader gains from it;
- the smallest set of steps that reproduces it, and against which version or commit;
- the configuration it needs, if it only appears under particular settings;
- whether you have told anyone else, and whether a disclosure date already binds you.

Please redact anything real — an example envelope with placeholder values explains a leak finding
better than a live one.

## What happens next

- We acknowledge a report within **five working days**.
- We tell you whether we can reproduce it, and what we think its severity is, as soon as we know.
- We keep you updated while a fix is prepared, and we agree a disclosure date with you. Our default
  is to publish an advisory once a fix is available, and in any case within **90 days** of the
  report — earlier if the problem is already public or being exploited.
- We credit you in the advisory unless you would rather stay anonymous.

There is no bug-bounty programme. We are grateful anyway, and we say so publicly.

## What we consider most serious

- An event silently lost: a publish that fails while the caller is told it succeeded, so the trail
  has a hole and nobody is aware of it.
- The defensive strip failing — a certificate, digest, OCSP or CRL response, archive material,
  validation blob or document bytes reaching the stream through an attribute key the strip does not
  recognise, or a bearer-token-shaped value surviving the publisher's strip.
- A national identifier, name, e-mail address or any other direct identifier reaching
  `DataSubjects`, where a pseudonymous internal reference is required.
- An event whose contents do not match what actually happened — wrong subject, wrong document
  reference, wrong outcome — because a chained trail makes that permanent and a later reader has
  nothing to compare it against.
- A missing or incorrect event identifier, occurrence time, or correlation and trace identifier:
  those are exactly what makes the referenced evidence findable, so a wrong one is a broken link,
  not a cosmetic defect.
- A way for a caller to suppress, forge or replay events through the fields it controls.

Denial of service and findings that need an already-compromised host are in scope but lower
priority. Reports about outdated dependencies are welcome where you can show the vulnerable path
is actually reachable.

## Scope

This policy covers the code in this repository. It does not cover the broker, the audit or evidence
sink that consumes the stream, the qualified trust service provider that holds the cryptographic
evidence, or the services that import this library — report those to the parties that operate them.

## Supported versions

Security fixes land on the most recent release. Older tags are not patched; if you are pinned to
one, the fix is to move forward. This module is pinned in lockstep with the platform kit, so a fix
may require moving that pin too.
