# golden

Real vendor API responses, captured once from a live tenant, anonymised, and
replayed through `httptest` in connector tests.

## Why this exists

Every existing connector test builds its mock response by marshalling our own
Go struct (or an equivalent `map[string]any` with the same keys). That
round-trips perfectly by construction: if a struct field is misnamed, points
at a field the vendor never sends, or the vendor's actual JSON nests a value
one level differently than we assume, a self-marshalled mock cannot catch it
— it only checks that our code can read back what our code wrote. Two
providers shipped exactly that defect (a struct field parsed by our code that
the live API never actually populated) and every existing test passed anyway.

A golden file is the antidote: it is real response *shape*, so decoding it
exercises the actual contract instead of our assumption about it. When a
golden-backed test asserts on `core.User` output, that assertion is the real
contract between the vendor and unseat.

## What's here

`testdata/<provider>-<endpoint>.json` — one file per captured endpoint,
holding the actual HTTP response body shape (even when the raw capture was
recorded one object per line for readability, the golden file re-wraps it
into the real response envelope, since that envelope is what the connector's
JSON decoder actually sees on the wire). Each preserves, from the real
response, exactly:

- the same keys, at the same nesting depth
- the same JSON types (string vs number vs bool vs array vs object)
- the same null-vs-absent distinction — whether a field is `null` or simply
  missing is often the signal a connector depends on, and is the single
  easiest thing to get wrong when hand-writing a mock
- 2-3 array entries, enough to see field variation (e.g. a user with no
  `primaryTeamId`, a guest with an external email) without carrying the whole
  tenant

## How to refresh a golden file

1. Capture a real response from the live API for the endpoint in question
   (e.g. via the provider's own `ListUsers`/`Billing` call with verbose
   logging, or a one-off authenticated request). Never commit the raw
   capture — treat it as containing live customer data.
2. Anonymise it (see rule below) down to 2-3 representative entries, keeping
   every key the real payload had, in the same nesting and types.
3. Replace the file under `testdata/`.
4. Run the connector's golden-backed test. If it now fails, that is the
   point of this exercise: the connector's struct assumed a field, or a
   shape, that the real API does not provide. Do not silently loosen the
   golden file to make the test pass — fix the connector, or if you cannot
   confirm the real shape against vendor documentation, leave the test
   failing and say so.

## Anonymisation rule

Replace every personal value (name, email, login, avatar URL, company name,
IDs that could be correlated back to a real person or account) with a
synthetic one. Keep the company domain generic (`example.com`).

**Never add a key the real payload did not contain, and never drop a key it
did.** Absence of a field is frequently the exact thing under test — e.g. a
provider's "unfiltered members" endpoint that structurally cannot carry a
`role` field, or a `lastActiveTime` field a struct expects but the API never
sends. Synthesising a missing field back in to "complete" the shape defeats
the entire purpose of a golden file.
