# Local copy of nntppool v5.0.0

Upstream: github.com/javi11/nntppool/v5 v5.0.0, unmodified except for:

- `expected_part.go` (new): `WithExpectedPart`.
- `reader.go`: part check before the body, no delivery for a wrong part, status 430 at the end.
- `nntp.go`: the decoder reads the expected part from the request context.

Why: some providers answer a message-ID with a different post that has a valid
yEnc CRC. Without a way to refuse it the pool never tries the next provider.
