package lambtrip

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
)

func TestLambtrip(t *testing.T) {
	t.Skip("skipping test") // this test is skipped because it requires a real AWS account

	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	svc := lambda.NewFromConfig(cfg)

	roundtripper := New(svc)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "lambda://url-compressor-CompressUrlFunction-RSmoCBggFdvq/", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := roundtripper.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(string(body))
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
