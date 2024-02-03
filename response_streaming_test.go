package lambtrip

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
)

func TestResponseStreaming(t *testing.T) {
	t.Skip("skipping test") // this test is skipped because it requires a real AWS account

	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	svc := lambda.NewFromConfig(cfg)

	roundtripper := NewResponseStreaming(svc)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "lambda://ridgenative-example-function-urls-ExampleApi-pD33rUEKGLuP/", nil)
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
