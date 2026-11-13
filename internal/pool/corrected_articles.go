package pool

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/javi11/nntppool/v5"
)

// ArticleCorrector remembers an article that some provider serves as a
// different post, with the yEnc part number the genuine article has.
type ArticleCorrector interface {
	CorrectArticle(messageID string, part int64)
}

// correctedArticles holds the few message-IDs proven at import to be served
// wrongly by at least one provider. Every other article is fetched exactly as
// before. The set is persisted so playback keeps working after a restart.
type correctedArticles struct {
	mu    sync.RWMutex
	parts map[string]int64
	path  string
}

var corrections = &correctedArticles{parts: map[string]int64{}}

// LoadCorrectedArticles reads the saved corrections and remembers path for new
// ones. A missing file is an empty set.
func LoadCorrectedArticles(path string) error {
	return corrections.load(path)
}

func (c *correctedArticles) load(path string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.path = path
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		id, raw, ok := strings.Cut(scanner.Text(), "\t")
		if !ok {
			continue
		}
		if part, err := strconv.ParseInt(raw, 10, 64); err == nil && part > 0 {
			c.parts[id] = part
		}
	}
	return scanner.Err()
}

func (c *correctedArticles) lookup(id string) int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.parts[id]
}

func (c *correctedArticles) add(id string, part int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if part <= 0 || c.parts[id] == part {
		return
	}
	c.parts[id] = part
	if c.path == "" {
		return
	}
	file, err := os.OpenFile(c.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
	if err == nil {
		_, err = fmt.Fprintf(file, "%s\t%d\n", id, part)
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
	}
	if err != nil {
		slog.Error("Could not save corrected article; it stays corrected until restart", "error", err)
	}
}

// correctingClient asks for the genuine part of corrected articles.
type correctingClient struct {
	NntpClient
	corrections *correctedArticles
}

var _ ArticleCorrector = (*correctingClient)(nil)

func (c *correctingClient) CorrectArticle(messageID string, part int64) {
	c.corrections.add(messageID, part)
}

func (c *correctingClient) expect(ctx context.Context, messageID string) context.Context {
	if part := c.corrections.lookup(messageID); part > 0 {
		return nntppool.WithExpectedPart(ctx, part)
	}
	return ctx
}

func (c *correctingClient) Fetch(ctx context.Context, r nntppool.Req) (*nntppool.ArticleBody, error) {
	return c.NntpClient.Fetch(c.expect(ctx, r.MessageID), r)
}

func (c *correctingClient) FetchAsync(ctx context.Context, r nntppool.Req) <-chan nntppool.BodyResult {
	return c.NntpClient.FetchAsync(c.expect(ctx, r.MessageID), r)
}

// Unwrap returns the client underneath the corrections layer.
func Unwrap(client NntpClient) NntpClient {
	if wrapped, ok := client.(*correctingClient); ok {
		return wrapped.NntpClient
	}
	return client
}
