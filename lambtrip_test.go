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
