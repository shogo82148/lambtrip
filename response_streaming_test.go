package lambtrip

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"testing/iotest"

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

// newInvokeWithResponseStreamResponseEventReader creates a new invokeWithResponseStreamResponseEventReader.
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

func newInvokeWithResponseStreamResponseEventReaderWithCustomCompleteEvent(chunks [][]byte, event types.InvokeWithResponseStreamCompleteEvent) *invokeWithResponseStreamResponseEventReader {
	ch := make(chan types.InvokeWithResponseStreamResponseEvent, len(chunks)+1)
	for _, chunk := range chunks {
		ch <- &types.InvokeWithResponseStreamResponseEventMemberPayloadChunk{
			Value: types.InvokeResponseStreamUpdate{
				Payload: chunk,
			},
		}
	}
	ch <- &types.InvokeWithResponseStreamResponseEventMemberInvokeComplete{
		Value: event,
	}
	close(ch)
	return &invokeWithResponseStreamResponseEventReader{ch: ch}
}

// newInvokeWithResponseStreamResponseEventReaderUnexpectedEOF creates a new invokeWithResponseStreamResponseEventReader with an unexpected EOF.
func newInvokeWithResponseStreamResponseEventReaderUnexpectedEOF(chunks [][]byte) *invokeWithResponseStreamResponseEventReader {
	ch := make(chan types.InvokeWithResponseStreamResponseEvent, len(chunks)+1)
	for _, chunk := range chunks {
		ch <- &types.InvokeWithResponseStreamResponseEventMemberPayloadChunk{
			Value: types.InvokeResponseStreamUpdate{
				Payload: chunk,
			},
		}
	}
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

func TestTransport(t *testing.T) {
	transport := &ResponseStreamTransport{
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

func TestTransport_OneByteReader(t *testing.T) {
	transport := &ResponseStreamTransport{
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

	r := iotest.OneByteReader(resp.Body)
	body, err := io.ReadAll(r)
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

func TestTransport_Copy(t *testing.T) {
	transport := &ResponseStreamTransport{
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

	buf := &bytes.Buffer{}
	_, err = io.Copy(buf, resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	if body != `"Hello, world!"` {
		t.Errorf("body = %q, want %q", body, `"Hello, world!"`)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestTransport_ErrUnexpectedEOFInPrelude(t *testing.T) {
	transport := &ResponseStreamTransport{
		lambda: func(ctx context.Context, params *lambda.InvokeWithResponseStreamInput, optFns ...func(*lambda.Options)) (*invokeWithResponseStreamOutput, error) {
			return &invokeWithResponseStreamOutput{
				Output: &lambda.InvokeWithResponseStreamOutput{
					StatusCode:                http.StatusOK,
					ResponseStreamContentType: aws.String("application/vnd.awslambda.http-integration-response"),
				},
				StreamGetter: GetStreamMock(func() *lambda.InvokeWithResponseStreamEventStream {
					stream := lambda.NewInvokeWithResponseStreamEventStream()
					stream.Reader = newInvokeWithResponseStreamResponseEventReaderUnexpectedEOF(nil)
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
	_, err = transport.RoundTrip(req)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("err = %v, want %v", err, io.ErrUnexpectedEOF)
	}
}

func TestTransport_ErrInPrelude(t *testing.T) {
	transport := &ResponseStreamTransport{
		lambda: func(ctx context.Context, params *lambda.InvokeWithResponseStreamInput, optFns ...func(*lambda.Options)) (*invokeWithResponseStreamOutput, error) {
			return &invokeWithResponseStreamOutput{
				Output: &lambda.InvokeWithResponseStreamOutput{
					StatusCode:                http.StatusOK,
					ResponseStreamContentType: aws.String("application/vnd.awslambda.http-integration-response"),
				},
				StreamGetter: GetStreamMock(func() *lambda.InvokeWithResponseStreamEventStream {
					completeEvent := types.InvokeWithResponseStreamCompleteEvent{
						ErrorCode:    aws.String("ERR"),
						ErrorDetails: aws.String("error message"),
					}
					stream := lambda.NewInvokeWithResponseStreamEventStream()
					stream.Reader = newInvokeWithResponseStreamResponseEventReaderWithCustomCompleteEvent(nil, completeEvent)
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
	_, err = transport.RoundTrip(req)

	var e *ResponseStreamError
	if !errors.As(err, &e) {
		t.Errorf("err = %v, want %v", err, e)
	}
	if e.ErrorCode != "ERR" {
		t.Errorf("e.ErrorCode = %q, want %q", e.ErrorCode, "ERR")
	}
	if e.ErrorDetails != "error message" {
		t.Errorf("e.ErrorDetails = %q, want %q", e.ErrorDetails, "error message")
	}
}

func TestTransport_ErrUnexpectedEOF(t *testing.T) {
	transport := &ResponseStreamTransport{
		lambda: func(ctx context.Context, params *lambda.InvokeWithResponseStreamInput, optFns ...func(*lambda.Options)) (*invokeWithResponseStreamOutput, error) {
			return &invokeWithResponseStreamOutput{
				Output: &lambda.InvokeWithResponseStreamOutput{
					StatusCode:                http.StatusOK,
					ResponseStreamContentType: aws.String("application/vnd.awslambda.http-integration-response"),
				},
				StreamGetter: GetStreamMock(func() *lambda.InvokeWithResponseStreamEventStream {
					stream := lambda.NewInvokeWithResponseStreamEventStream()
					stream.Reader = newInvokeWithResponseStreamResponseEventReaderUnexpectedEOF([][]byte{
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
	_, err = io.ReadAll(resp.Body)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("err = %v, want %v", err, io.ErrUnexpectedEOF)
	}
}

func TestTransport_ErrorDuringResponse(t *testing.T) {
	transport := &ResponseStreamTransport{
		lambda: func(ctx context.Context, params *lambda.InvokeWithResponseStreamInput, optFns ...func(*lambda.Options)) (*invokeWithResponseStreamOutput, error) {
			return &invokeWithResponseStreamOutput{
				Output: &lambda.InvokeWithResponseStreamOutput{
					StatusCode:                http.StatusOK,
					ResponseStreamContentType: aws.String("application/vnd.awslambda.http-integration-response"),
				},
				StreamGetter: GetStreamMock(func() *lambda.InvokeWithResponseStreamEventStream {
					completeEvent := types.InvokeWithResponseStreamCompleteEvent{
						ErrorCode:    aws.String("ERR"),
						ErrorDetails: aws.String("error message"),
					}
					stream := lambda.NewInvokeWithResponseStreamEventStream()
					stream.Reader = newInvokeWithResponseStreamResponseEventReaderWithCustomCompleteEvent([][]byte{
						[]byte(`{}`),
						{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
					}, completeEvent)
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
	_, err = io.ReadAll(resp.Body)

	var e *ResponseStreamError
	if !errors.As(err, &e) {
		t.Errorf("err = %v, want %v", err, e)
	}
	if e.ErrorCode != "ERR" {
		t.Errorf("e.ErrorCode = %q, want %q", e.ErrorCode, "ERR")
	}
	if e.ErrorDetails != "error message" {
		t.Errorf("e.ErrorDetails = %q, want %q", e.ErrorDetails, "error message")
	}
}
