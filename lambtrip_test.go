package lambtrip

import (
	"context"
	"net/http"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/lambda"
)

var _ invokeAPIClient = InvokeMock(nil)

type InvokeMock func(ctx context.Context, params *lambda.InvokeInput, optFns ...func(*lambda.Options)) (*lambda.InvokeOutput, error)

func (m InvokeMock) Invoke(ctx context.Context, params *lambda.InvokeInput, optFns ...func(*lambda.Options)) (*lambda.InvokeOutput, error) {
	return m(ctx, params, optFns...)
}

func TestTransport(t *testing.T) {
	transport := &Transport{
		lambda: InvokeMock(func(ctx context.Context, params *lambda.InvokeInput, optFns ...func(*lambda.Options)) (*lambda.InvokeOutput, error) {
			return &lambda.InvokeOutput{
				StatusCode: http.StatusOK,
				Payload:    []byte(`{}`),
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
}

func TestIsBinary(t *testing.T) {
	tests := []struct {
		contentType string
		want        bool
	}{
		{"text/html", false},
		{"text/plain", false},
		{"text/xml", false},
		{"application/json", false},
		{"application/javascript", false},
		{"application/xml", false},
		{"application/foo+json", false},
		{"application/foo+xml", false},
		{"application/foo+xml ; charset=utf8", false},
		{"application/octet-stream", true},
	}

	for _, tt := range tests {
		got := isBinary(tt.contentType)
		if got != tt.want {
			t.Errorf("isBinary(%q) = %v, want %v", tt.contentType, got, tt.want)
		}
	}
}
