package nntppool

import "context"

type expectedPartKey struct{}

// WithExpectedPart makes article fetches under ctx accept only the yEnc part
// number given. Any other part is another post served under this message-ID:
// its body is drained without reaching the caller and the provider counts as
// not having the article, so the next provider is tried. The check runs before
// the first decoded byte, which keeps it safe for streamed fetches.
//
// Local change to upstream nntppool v5.0.0; see third_party/nntppool/LOCAL.md.
func WithExpectedPart(ctx context.Context, part int64) context.Context {
	return context.WithValue(ctx, expectedPartKey{}, part)
}

// ExpectedPart returns the part set by WithExpectedPart, or 0.
func ExpectedPart(ctx context.Context) int64 {
	part, _ := ctx.Value(expectedPartKey{}).(int64)
	return part
}

func (r *NNTPResponse) checkExpectedPart() {
	if r.expectedPart > 0 && r.YEnc.Part != r.expectedPart {
		r.wrongPart = true
		r.onMeta = nil
	}
}
