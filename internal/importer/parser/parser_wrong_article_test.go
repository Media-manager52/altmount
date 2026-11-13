package parser

import (
	"context"
	"testing"
	"time"

	"github.com/javi11/nntppool/v5"
	"github.com/javi11/nzbparser"
	"github.com/kipsilabs/altmount/internal/pool"
	"github.com/kipsilabs/altmount/internal/testsupport/fakepool"
)

type wrongArticleClient struct {
	pool.NntpClient
	genuine   *nntppool.ArticleBody
	err       error
	asked     []int64
	corrected map[string]int64
}

func (c *wrongArticleClient) Fetch(ctx context.Context, _ nntppool.Req) (*nntppool.ArticleBody, error) {
	c.asked = append(c.asked, nntppool.ExpectedPart(ctx))
	return c.genuine, c.err
}

func (c *wrongArticleClient) CorrectArticle(id string, part int64) { c.corrected[id] = part }

func TestWrongFirstArticleIsReplacedAndRemembered(t *testing.T) {
	p := NewParser(newFakeFullPoolManager(fakepool.New()), stormConfigGetter(2))
	wrong := &nntppool.ArticleBody{Bytes: []byte("decoy"), YEnc: nntppool.YEncMeta{Part: 234}}
	genuine := &nntppool.ArticleBody{Bytes: []byte("Rar!"), YEnc: nntppool.YEncMeta{Part: 1}}
	client := &wrongArticleClient{genuine: genuine, corrected: map[string]int64{}}

	got := p.correctWrongFirstArticle(context.Background(), client, nzbparser.NzbSegment{ID: "a@b", Number: 1}, time.Time{}, wrong)
	if got != genuine {
		t.Fatalf("got %q, want the genuine article", got.Bytes)
	}
	if len(client.asked) != 1 || client.asked[0] != 1 || client.corrected["a@b"] != 1 {
		t.Fatalf("asked=%v corrected=%v", client.asked, client.corrected)
	}
	if head, ok := p.heads.get("a@b"); !ok || string(head.bytes) != "Rar!" {
		t.Fatalf("head cache still holds the wrong post: %q", head.bytes)
	}
}

func TestWrongFirstArticleWithNoGenuineCopyKeepsTheOriginalOutcome(t *testing.T) {
	p := NewParser(newFakeFullPoolManager(fakepool.New()), stormConfigGetter(2))
	wrong := &nntppool.ArticleBody{Bytes: []byte("decoy"), YEnc: nntppool.YEncMeta{Part: 234}}
	client := &wrongArticleClient{err: nntppool.ErrArticleNotFound, corrected: map[string]int64{}}

	if got := p.correctWrongFirstArticle(context.Background(), client, nzbparser.NzbSegment{ID: "a@b", Number: 1}, time.Time{}, wrong); got != wrong {
		t.Fatalf("got %q, want the original result", got.Bytes)
	}
	if len(client.corrected) != 0 {
		t.Fatalf("an unproven article was remembered: %v", client.corrected)
	}
}

func TestMatchingOrUnnumberedFirstArticleCostsNoExtraFetch(t *testing.T) {
	p := NewParser(newFakeFullPoolManager(fakepool.New()), stormConfigGetter(2))
	client := &wrongArticleClient{corrected: map[string]int64{}}
	for _, part := range []int64{1, 0} {
		result := &nntppool.ArticleBody{YEnc: nntppool.YEncMeta{Part: part}}
		if got := p.correctWrongFirstArticle(context.Background(), client, nzbparser.NzbSegment{ID: "a@b", Number: 1}, time.Time{}, result); got != result {
			t.Fatal("result was replaced")
		}
	}
	if len(client.asked) != 0 {
		t.Fatalf("extra fetches: %v", client.asked)
	}
}
