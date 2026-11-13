package nntppool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"net"
	"strings"
	"testing"
	"time"
)

func yencArticle(part int, payload []byte) string {
	encoded := make([]byte, len(payload))
	for i, b := range payload {
		encoded[i] = b + 42
	}
	return fmt.Sprintf("=ybegin part=%d total=9 line=128 size=9000 name=x.bin\r\n=ypart begin=1 end=%d\r\n%s\r\n=yend size=%d part=%d pcrc32=%08x\r\n",
		part, len(payload), encoded, len(payload), part, crc32.ChecksumIEEE(payload))
}

func articleProvider(part int, payload []byte) Provider {
	return Provider{Connections: 1, Factory: func(ctx context.Context) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			_, _ = server.Write([]byte("200 server ready\r\n"))
			buf := make([]byte, 4096)
			for {
				n, err := server.Read(buf)
				if err != nil {
					return
				}
				switch cmd := string(buf[:n]); {
				case strings.HasPrefix(cmd, "DATE"):
					_, _ = server.Write([]byte("111 20240315120000\r\n"))
				case strings.HasPrefix(cmd, "BODY"):
					_, _ = fmt.Fprintf(server, "222 0 <id@test>\r\n%s.\r\n", yencArticle(part, payload))
				default:
					_, _ = server.Write([]byte("500 unknown\r\n"))
				}
			}
		}()
		return client, nil
	}}
}

func expectedPartClient(t *testing.T, providers ...Provider) *Client {
	t.Helper()
	client, err := NewClient(context.Background(), providers, WithDispatchStrategy(DispatchFIFO))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestExpectedPartFailsOverToTheGenuineArticle(t *testing.T) {
	wrong, genuine := bytes.Repeat([]byte("W"), 64), bytes.Repeat([]byte("G"), 96)
	client := expectedPartClient(t, articleProvider(234, wrong), articleProvider(1, genuine))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	body, err := client.Fetch(ctx, Req{MessageID: "id@test"})
	if err != nil || !bytes.Equal(body.Bytes, wrong) {
		t.Fatalf("without an expected part the first provider answers as before: err=%v body=%q", err, body.Bytes)
	}
	body, err = client.Fetch(WithExpectedPart(ctx, 1), Req{MessageID: "id@test"})
	if err != nil || !bytes.Equal(body.Bytes, genuine) || body.YEnc.Part != 1 {
		t.Fatalf("buffered: err=%v body=%q", err, body.Bytes)
	}
	var streamed bytes.Buffer
	if _, err := client.Fetch(WithExpectedPart(ctx, 1), Req{MessageID: "id@test", Writer: &streamed}); err != nil || !bytes.Equal(streamed.Bytes(), genuine) {
		t.Fatalf("streamed: err=%v bytes=%q", err, streamed.Bytes())
	}
}

func TestExpectedPartReportsNotFoundWithoutDeliveringWrongBytes(t *testing.T) {
	client := expectedPartClient(t, articleProvider(234, bytes.Repeat([]byte("W"), 64)))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	metaCalled := false
	var streamed bytes.Buffer
	_, err := client.Fetch(WithExpectedPart(ctx, 1), Req{MessageID: "id@test", Writer: &streamed, OnMeta: func(YEncMeta) { metaCalled = true }})
	if !errors.Is(err, ErrArticleNotFound) {
		t.Fatalf("err=%v, want ErrArticleNotFound", err)
	}
	if streamed.Len() != 0 || metaCalled {
		t.Fatalf("wrong post reached the caller: %d bytes, meta=%v", streamed.Len(), metaCalled)
	}
}
