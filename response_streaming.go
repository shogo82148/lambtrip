package lambtrip

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

var separate = []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}

var _ invokeWithResponseStreamAPIClient = (*lambda.Client)(nil)

type invokeWithResponseStreamAPIClient interface {
	InvokeWithResponseStream(ctx context.Context, params *lambda.InvokeWithResponseStreamInput, optFns ...func(*lambda.Options)) (*lambda.InvokeWithResponseStreamOutput, error)
}

var _ http.RoundTripper = (*ResponseStreaming)(nil)

type ResponseStreaming struct {
	lambda invokeWithResponseStreamAPIClient
}

func NewResponseStreaming(c *lambda.Client) *ResponseStreaming {
	return &ResponseStreaming{
		lambda: c,
	}
}

func (t *ResponseStreaming) RoundTrip(req *http.Request) (*http.Response, error) {
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
	out, err := t.lambda.InvokeWithResponseStream(ctx, &lambda.InvokeWithResponseStreamInput{
		FunctionName: aws.String(req.URL.Host),
		Payload:      payload,
	})
	if err != nil {
		return nil, err
	}

	stream := out.GetStream()
	resp, buf, err := handleStreamingPrelude(ctx, stream)
	if err != nil {
		return nil, err
	}

	return &http.Response{
		StatusCode: resp.StatusCode,
		Proto:      "HTTP/1.0",
		ProtoMajor: 1,
		ProtoMinor: 0,
		Body:       &streamingBody{ctx, buf, stream},
		Request:    req,
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
		return 0, io.EOF
	case *types.InvokeWithResponseStreamResponseEventMemberPayloadChunk:
		n := copy(p, event.Value.Payload)
		b.buf = event.Value.Payload[n:]
		return n, b.stream.Err()
	}
	return 0, io.ErrUnexpectedEOF
}

func (b *streamingBody) Close() error {
	return b.stream.Close()
}
