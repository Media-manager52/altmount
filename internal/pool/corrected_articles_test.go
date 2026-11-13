package pool

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/javi11/nntppool/v5"
)

type expectationRecorder struct {
	NntpClient
	seen map[string]int64
}

func (r *expectationRecorder) Fetch(ctx context.Context, req nntppool.Req) (*nntppool.ArticleBody, error) {
	r.seen[req.MessageID] = nntppool.ExpectedPart(ctx)
	return &nntppool.ArticleBody{}, nil
}

func (r *expectationRecorder) FetchAsync(ctx context.Context, req nntppool.Req) <-chan nntppool.BodyResult {
	r.seen[req.MessageID] = nntppool.ExpectedPart(ctx)
	ch := make(chan nntppool.BodyResult, 1)
	ch <- nntppool.BodyResult{Body: &nntppool.ArticleBody{}}
	close(ch)
	return ch
}

func TestCorrectedArticlesAreAskedForTheirPartAndOthersAreUntouched(t *testing.T) {
	recorder := &expectationRecorder{seen: map[string]int64{}}
	client := &correctingClient{NntpClient: recorder, corrections: &correctedArticles{parts: map[string]int64{}}}
	client.CorrectArticle("bad@id", 1)

	if _, err := client.Fetch(context.Background(), nntppool.Req{MessageID: "bad@id"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Fetch(context.Background(), nntppool.Req{MessageID: "normal@id"}); err != nil {
		t.Fatal(err)
	}
	<-client.FetchAsync(context.Background(), nntppool.Req{MessageID: "async-bad@id"})
	if recorder.seen["bad@id"] != 1 || recorder.seen["normal@id"] != 0 || recorder.seen["async-bad@id"] != 0 {
		t.Fatalf("seen=%v", recorder.seen)
	}
	client.CorrectArticle("async-bad@id", 3)
	<-client.FetchAsync(context.Background(), nntppool.Req{MessageID: "async-bad@id"})
	if recorder.seen["async-bad@id"] != 3 {
		t.Fatalf("FetchAsync expectation=%d, want 3", recorder.seen["async-bad@id"])
	}
	if Unwrap(client) != NntpClient(recorder) {
		t.Fatal("Unwrap did not return the underlying client")
	}
}

func TestCorrectedArticlesSurviveARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrected_articles.tsv")
	first := &correctedArticles{parts: map[string]int64{}}
	if err := first.load(path); err != nil {
		t.Fatal(err)
	}
	first.add("bad@id", 1)
	first.add("bad@id", 1)
	first.add("ignored@id", 0)

	second := &correctedArticles{parts: map[string]int64{}}
	if err := second.load(path); err != nil {
		t.Fatal(err)
	}
	if second.lookup("bad@id") != 1 || second.lookup("ignored@id") != 0 || len(second.parts) != 1 {
		t.Fatalf("parts=%v", second.parts)
	}
}
