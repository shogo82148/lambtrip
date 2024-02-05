package lambtrip

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/lambda"
)

var _ invokeAPIClient = InvokeMock(nil)

type InvokeMock func(ctx context.Context, params *lambda.InvokeInput, optFns ...func(*lambda.Options)) (*lambda.InvokeOutput, error)

func (m InvokeMock) Invoke(ctx context.Context, params *lambda.InvokeInput, optFns ...func(*lambda.Options)) (*lambda.InvokeOutput, error) {
	return m(ctx, params, optFns...)
}

func TestBufferedTransport(t *testing.T) {
	transport := &BufferedTransport{
		lambda: InvokeMock(func(ctx context.Context, params *lambda.InvokeInput, optFns ...func(*lambda.Options)) (*lambda.InvokeOutput, error) {
			var req request
			if err := json.Unmarshal(params.Payload, &req); err != nil {
				return nil, err
			}
			if req.Version != "2.0" {
				t.Errorf("req.Version = %q, want %q", req.Version, "2.0")
			}
			if req.RouteKey != "$default" {
				t.Errorf("req.RouteKey = %q, want %q", req.RouteKey, "$default")
			}
			if req.HTTPMethod != http.MethodGet {
				t.Errorf("req.HTTPMethod = %q, want %q", req.HTTPMethod, http.MethodGet)
			}
			if req.Body != "" {
				t.Errorf("req.Body = %q, want %q", req.Body, "")
			}
			if req.IsBase64Encoded {
				t.Errorf("req.IsBase64Encoded = %v, want %v", req.IsBase64Encoded, false)
			}
			if req.RawPath != "/foo/bar" {
				t.Errorf("req.RawPath = %q, want %q", req.RawPath, "/foo/bar")
			}
			if req.RawQueryString != "" {
				t.Errorf("req.RawQueryString = %q, want %q", req.RawQueryString, "")
			}
			if req.RequestContext.HTTP.Method != http.MethodGet {
				t.Errorf("req.RequestContext.HTTP.Method = %q, want %q", req.RequestContext.HTTP.Method, http.MethodGet)
			}
			if req.RequestContext.HTTP.Path != "/foo/bar" {
				t.Errorf("req.RequestContext.HTTP.Path = %q, want %q", req.RequestContext.HTTP.Path, "/foo/bar")
			}
			if req.RequestContext.HTTP.Protocol != "HTTP/1.0" {
				t.Errorf("req.RequestContext.HTTP.Protocol = %q, want %q", req.RequestContext.HTTP.Protocol, "HTTP/1.0")
			}
			if req.RequestContext.RequestID == "" {
				t.Errorf("req.RequestContext.RequestID = %q, want non-empty", req.RequestContext.RequestID)
			}
			if req.RequestContext.TimeEpoch == 0 {
				t.Errorf("req.RequestContext.TimeEpoch = %d, want non-zero", req.RequestContext.TimeEpoch)
			}

			return &lambda.InvokeOutput{
				StatusCode: http.StatusOK,
				Payload:    []byte(`{"body": "\"Hello, world!\""}`),
			}, nil
		}),
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "lambda://function-name/foo/bar", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}

	if resp.Status != "200 OK" {
		t.Errorf("resp.Status = %q, want %q", resp.Status, "200 OK")
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("resp.StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if resp.Proto != "HTTP/1.0" {
		t.Errorf("resp.Proto = %q, want %q", resp.Proto, "HTTP/1.0")
	}
	if resp.ProtoMajor != 1 {
		t.Errorf("resp.ProtoMajor = %d, want %d", resp.ProtoMajor, 1)
	}
	if resp.ProtoMinor != 0 {
		t.Errorf("resp.ProtoMinor = %d, want %d", resp.ProtoMinor, 0)
	}
	if resp.Request != req {
		t.Errorf("resp.Request = %v, want %v", resp.Request, req)
	}
	if resp.Header.Get("Content-Type") != "application/json" {
		t.Errorf("resp.Header.Get(%q) = %q, want %q", "Content-Type", resp.Header.Get("Content-Type"), "application/json")
	}
	if resp.Header.Get("Content-Length") != "15" {
		t.Errorf("resp.Header.Get(%q) = %q, want %q", "Content-Length", resp.Header.Get("Content-Length"), "15")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `"Hello, world!"` {
		t.Errorf("body = %q, want %q", string(body), `"Hello, world!"`)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestBufferedTransport_Base64Response(t *testing.T) {
	transport := &BufferedTransport{
		lambda: InvokeMock(func(ctx context.Context, params *lambda.InvokeInput, optFns ...func(*lambda.Options)) (*lambda.InvokeOutput, error) {
			return &lambda.InvokeOutput{
				StatusCode: http.StatusOK,
				Payload:    []byte(`{"body": "IkhlbGxvLCB3b3JsZCEi", "isBase64Encoded": true}`),
			}, nil
		}),
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "lambda://function-name/foo/bar", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("resp.StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if resp.Header.Get("Content-Type") != "application/json" {
		t.Errorf("resp.Header.Get(%q) = %q, want %q", "Content-Type", resp.Header.Get("Content-Type"), "application/json")
	}
	if resp.Header.Get("Content-Length") != "15" {
		t.Errorf("resp.Header.Get(%q) = %q, want %q", "Content-Length", resp.Header.Get("Content-Length"), "15")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `"Hello, world!"` {
		t.Errorf("body = %q, want %q", string(body), `"Hello, world!"`)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestBufferedTransport_Forbidden(t *testing.T) {
	transport := &BufferedTransport{
		lambda: InvokeMock(func(ctx context.Context, params *lambda.InvokeInput, optFns ...func(*lambda.Options)) (*lambda.InvokeOutput, error) {
			return &lambda.InvokeOutput{
				StatusCode: http.StatusForbidden,
				Payload:    []byte(`{"body": "\"Hello, world!\""}`),
			}, nil
		}),
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "lambda://function-name/foo/bar", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = transport.RoundTrip(req)

	var myErr *LambdaError
	if !errors.As(err, &myErr) {
		t.Fatalf("unexpected error type: %T", err)
	}
	if myErr.StatusCode != http.StatusForbidden {
		t.Errorf("myErr.StatusCode = %d, want %d", myErr.StatusCode, http.StatusForbidden)
	}
	if string(myErr.Payload) != `{"body": "\"Hello, world!\""}` {
		t.Errorf("myErr.Payload = %q, want %q", string(myErr.Payload), `{"body": "\"Hello, world!\""}`)
	}
}

func TestIsBinary(t *testing.T) {
	tests := []struct {
		header http.Header
		want   bool
	}{
		// text/html but encoded as gzip
		{
			header: http.Header{
				"Content-Type":     []string{"text/html; charset=utf-8"},
				"Content-Encoding": []string{"gzip"},
			},
			want: true,
		},

		// text/html and not encoded
		{
			header: http.Header{
				"Content-Type":     []string{"text/html; charset=utf-8"},
				"Content-Encoding": []string{"identity"},
			},
			want: false,
		},

		// text/*
		{
			header: http.Header{
				"Content-Type": []string{"text/html"},
			},
			want: false,
		},
		{
			header: http.Header{
				"Content-Type": []string{"text/plain"},
			},
			want: false,
		},
		{
			header: http.Header{
				"Content-Type": []string{"text/xml"},
			},
			want: false,
		},

		// data formats that are encoded as text
		{
			header: http.Header{
				"Content-Type": []string{"application/json"},
			},
			want: false,
		},
		{
			header: http.Header{
				"Content-Type": []string{"application/javascript"},
			},
			want: false,
		},
		{
			header: http.Header{
				"Content-Type": []string{"application/yaml"},
			},
			want: false,
		},
		{
			header: http.Header{
				"Content-Type": []string{"application/xml"},
			},
			want: false,
		},

		// custom media types that are encoded as text
		{
			header: http.Header{
				"Content-Type": []string{"application/foo+json"},
			},
			want: false,
		},
		{
			header: http.Header{
				"Content-Type": []string{"application/foo+yaml"},
			},
			want: false,
		},
		{
			header: http.Header{
				"Content-Type": []string{"application/foo+xml"},
			},
			want: false,
		},
		{
			header: http.Header{
				"Content-Type": []string{"application/foo+xml ; charset=utf8"},
			},
			want: false,
		},

		// common binary formats
		{
			header: http.Header{
				"Content-Type": []string{"application/octet-stream"},
			},
			want: true,
		},
		{
			header: http.Header{
				"Content-Type": []string{"image/jpeg"},
			},
			want: true,
		},
		{
			header: http.Header{
				"Content-Type": []string{"audio/mpeg"},
			},
			want: true,
		},
		{
			header: http.Header{
				"Content-Type": []string{"unknown-content-type"},
			},
			want: true,
		},
		{
			header: http.Header{
				"Content-Type": []string{""},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		got := isBinary(tt.header)
		if got != tt.want {
			t.Errorf("isBinary(%v) = %v, want %v", tt.header, got, tt.want)
		}
	}
}

func TestNewRequestID(t *testing.T) {
	re := regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}`)
	const count = 1000
	m := make(map[string]bool, count)
	for i := 0; i < count; i++ {
		id, err := newRequestID()
		if err != nil {
			t.Fatal(err)
		}
		if !re.MatchString(id) {
			t.Errorf("invalid request ID: %q", id)
		}
		if m[id] {
			t.Errorf("duplicate request ID: %q", id)
		}
		m[id] = true
	}
}
