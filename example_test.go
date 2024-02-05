package lambtrip_test

import (
	"context"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/shogo82148/lambtrip"
)

func Example() {
	// Initialize AWS SDK to create a service client for AWS Lambda.
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		panic(err)
	}
	svc := lambda.NewFromConfig(cfg)

	// Create a new HTTP transport and register the "lambda" protocol with a custom transport handler.
	t := &http.Transport{}
	t.RegisterProtocol("lambda", lambtrip.NewBufferedTransport(svc))
	// Create a new HTTP client using the custom transport to handle requests to Lambda functions.
	c := &http.Client{Transport: t}

	// Make an HTTP GET request to a specific Lambda function using the custom "lambda://" protocol.
	resp, err := c.Get("lambda://function-name/foo/bar")
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
}
