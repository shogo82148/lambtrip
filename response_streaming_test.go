package lambtrip

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

var _ streamGetter = GetStreamMock(nil)

type GetStreamMock func() *lambda.InvokeWithResponseStreamEventStream

func (m GetStreamMock) GetStream() *lambda.InvokeWithResponseStreamEventStream {
	return m()
}

var _ lambda.InvokeWithResponseStreamResponseEventReader = (*invokeWithResponseStreamResponseEventReader)(nil)

func newInvokeWithResponseStreamResponseEventReader(chunks [][]byte) *invokeWithResponseStreamResponseEventReader {
	ch := make(chan types.InvokeWithResponseStreamResponseEvent, len(chunks)+1)
	for _, chunk := range chunks {
		ch <- &types.InvokeWithResponseStreamResponseEventMemberPayloadChunk{
			Value: types.InvokeResponseStreamUpdate{
				Payload: chunk,
			},
		}
	}
	ch <- &types.InvokeWithResponseStreamResponseEventMemberInvokeComplete{}
	close(ch)
	return &invokeWithResponseStreamResponseEventReader{ch: ch}
}

type invokeWithResponseStreamResponseEventReader struct {
	ch chan types.InvokeWithResponseStreamResponseEvent
}

func (r *invokeWithResponseStreamResponseEventReader) Events() <-chan types.InvokeWithResponseStreamResponseEvent {
	return r.ch
}

func (r *invokeWithResponseStreamResponseEventReader) Close() error {
	return nil
}

func (r *invokeWithResponseStreamResponseEventReader) Err() error {
	return nil
}

func TestResponseStreaming(t *testing.T) {
	transport := &ResponseStreaming{
		lambda: func(ctx context.Context, params *lambda.InvokeWithResponseStreamInput, optFns ...func(*lambda.Options)) (*invokeWithResponseStreamOutput, error) {
			return &invokeWithResponseStreamOutput{
				Output: &lambda.InvokeWithResponseStreamOutput{
					StatusCode:                http.StatusOK,
					ResponseStreamContentType: aws.String("application/vnd.awslambda.http-integration-response"),
				},
				StreamGetter: GetStreamMock(func() *lambda.InvokeWithResponseStreamEventStream {
					stream := lambda.NewInvokeWithResponseStreamEventStream()
					stream.Reader = newInvokeWithResponseStreamResponseEventReader([][]byte{
						[]byte(`{}`),
						{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
						[]byte(`"Hello, world!"`),
					})
					return stream
				}),
			}, nil
		},
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com/foo/bar", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}

	if resp.Header.Get("Content-Type") != "application/json" {
		t.Errorf("resp.Header.Get(%q) = %q, want %q", "Content-Type", resp.Header.Get("Content-Type"), "application/json")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `"Hello, world!"` {
		t.Errorf("body = %q, want %q", body, `"Hello, world!"`)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
}
