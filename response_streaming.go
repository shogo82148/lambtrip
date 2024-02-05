package lambtrip

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

// ResponseStreamError is an error during response stream.
type ResponseStreamError struct {
	ErrorCode    string
	ErrorDetails string
}

func (e *ResponseStreamError) Error() string {
	return fmt.Sprintf("lambtrip: error during response stream: %s, %s", e.ErrorCode, e.ErrorDetails)
}

var separate = []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}

var _ streamGetter = (*lambda.InvokeWithResponseStreamOutput)(nil)

type streamGetter interface {
	GetStream() *lambda.InvokeWithResponseStreamEventStream
}

type invokeWithResponseStreamOutput struct {
	Output       *lambda.InvokeWithResponseStreamOutput
	StreamGetter streamGetter
}

var _ http.RoundTripper = (*ResponseStreamTransport)(nil)

type ResponseStreamTransport struct {
	lambda func(ctx context.Context, params *lambda.InvokeWithResponseStreamInput, optFns ...func(*lambda.Options)) (*invokeWithResponseStreamOutput, error)
}

func NewResponseStreamTransport(c *lambda.Client) *ResponseStreamTransport {
	return &ResponseStreamTransport{
		lambda: func(ctx context.Context, params *lambda.InvokeWithResponseStreamInput, optFns ...func(*lambda.Options)) (*invokeWithResponseStreamOutput, error) {
			out, err := c.InvokeWithResponseStream(ctx, params, optFns...)
			if err != nil {
				return nil, err
			}
			return &invokeWithResponseStreamOutput{Output: out, StreamGetter: out}, nil
		},
	}
}

func (t *ResponseStreamTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()

	// build the request
	r, err := buildRequest(req)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}

	// invoke the lambda
	out, err := t.lambda(ctx, &lambda.InvokeWithResponseStreamInput{
		FunctionName: aws.String(req.URL.Host),
		Payload:      payload,
	})
	if err != nil {
		return nil, err
	}
	stream := out.StreamGetter.GetStream()

	// handle the http-integration-response
	resp, buf, err := handleStreamingPrelude(ctx, stream)
	if err != nil {
		return nil, err
	}

	return &http.Response{
		Status:        resp.status(),
		StatusCode:    resp.statusCode(),
		Proto:         "HTTP/1.0",
		ProtoMajor:    1,
		ProtoMinor:    0,
		Header:        resp.header(),
		ContentLength: -1,
		Body:          &streamingBody{ctx, buf, stream},
		Close:         true,
		Request:       req,
	}, nil
}

func handleStreamingPrelude(ctx context.Context, stream *lambda.InvokeWithResponseStreamEventStream) (*response, []byte, error) {
	buf := []byte{}
	idx := -1
LOOP:
	for {
		var event types.InvokeWithResponseStreamResponseEvent
		select {
		case <-ctx.Done():
			stream.Close()
			return nil, nil, ctx.Err()
		case event = <-stream.Events():
		}

		switch event := event.(type) {
		case *types.InvokeWithResponseStreamResponseEventMemberInvokeComplete:
			stream.Close()
			return nil, nil, io.ErrUnexpectedEOF
		case *types.InvokeWithResponseStreamResponseEventMemberPayloadChunk:
			buf = append(buf, event.Value.Payload...)
			idx = bytes.Index(buf, separate)
			if idx >= 0 {
				break LOOP
			}
		default:
			return nil, nil, fmt.Errorf("lambtrip: unexpected event type: %T", event)
		}
	}

	prelude := buf[:idx]
	buf = buf[idx+len(separate):]

	var resp response
	if err := json.Unmarshal(prelude, &resp); err != nil {
		return nil, nil, err
	}
	return &resp, buf, nil
}

var _ io.ReadCloser = (*streamingBody)(nil)

type streamingBody struct {
	ctx    context.Context
	buf    []byte
	stream *lambda.InvokeWithResponseStreamEventStream
}

func (b *streamingBody) Read(p []byte) (int, error) {
	if len(b.buf) > 0 {
		n := copy(p, b.buf)
		b.buf = b.buf[n:]
		return n, nil
	}

	var event types.InvokeWithResponseStreamResponseEvent
	select {
	case <-b.ctx.Done():
		return 0, b.ctx.Err()
	case event = <-b.stream.Events():
	}

	switch event := event.(type) {
	case *types.InvokeWithResponseStreamResponseEventMemberInvokeComplete:
		if event.Value.ErrorCode != nil || event.Value.ErrorDetails != nil {
			return 0, &ResponseStreamError{
				ErrorCode:    aws.ToString(event.Value.ErrorCode),
				ErrorDetails: aws.ToString(event.Value.ErrorDetails),
			}
		}
		return 0, io.EOF
	case *types.InvokeWithResponseStreamResponseEventMemberPayloadChunk:
		n := copy(p, event.Value.Payload)
		b.buf = event.Value.Payload[n:]
		return n, b.stream.Err()
	case nil:
		return 0, io.ErrUnexpectedEOF
	default:
		return 0, fmt.Errorf("lambtrip: unexpected event type: %T", event)
	}
}

func (b *streamingBody) Close() error {
	return b.stream.Close()
}
