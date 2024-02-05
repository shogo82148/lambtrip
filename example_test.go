package lambtrip_test

import (
	"context"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/shogo82148/lambtrip"
)

func Example() {
	// initialize AWS SDK
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		panic(err)
	}
	svc := lambda.NewFromConfig(cfg)

	// register the lambda protocol
	t := &http.Transport{}
	t.RegisterProtocol("lambda", lambtrip.NewBufferedTransport(svc))
	c := &http.Client{Transport: t}

	// send a request to the lambda function
	resp, err := c.Get("lambda://function-name/foo/bar")
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
}
