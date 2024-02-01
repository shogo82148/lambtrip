package lambtrip

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
)

var _ invokeAPIClient = (*lambda.Client)(nil)

type invokeAPIClient interface {
	Invoke(ctx context.Context, params *lambda.InvokeInput, optFns ...func(*lambda.Options)) (*lambda.InvokeOutput, error)
}

// var _ invokeWithResponseStreamAPIClient = (*lambda.Client)(nil)

// type invokeWithResponseStreamAPIClient interface {
// 	InvokeWithResponseStream(ctx context.Context, params *lambda.InvokeWithResponseStreamInput, optFns ...func(*lambda.Options)) (*lambda.InvokeWithResponseStreamOutput, error)
// }

type request struct {
	Version         string            `json:"version"`
	RouteKey        string            `json:"routeKey"`
	HTTPMethod      string            `json:"httpMethod"`
	Body            string            `json:"body"`
	IsBase64Encoded bool              `json:"isBase64Encoded"`
	RawPath         string            `json:"rawPath"`
	RawQueryString  string            `json:"rawQueryString"`
	Headers         map[string]string `json:"headers"`
	Cookies         []string          `json:"cookies"`
	RequestContext  *requestContext   `json:"requestContext"`
}

type requestContext struct {
	HTTP      *requestContextHTTP `json:"http"`
	RequestID string              `json:"requestId,omitempty"`
	Stage     string              `json:"stage,omitempty"`
	Time      string              `json:"time,omitempty"`
	TimeEpoch int64               `json:"timeEpoch,omitempty"`
}

type requestContextHTTP struct {
	Method    string `json:"method,omitempty"`
	Path      string `json:"path,omitempty"`
	Protocol  string `json:"protocol,omitempty"`
	SourceIP  string `json:"sourceIp,omitempty"`
	UserAgent string `json:"userAgent,omitempty"`
}

type response struct {
	StatusCode      int               `json:"statusCode"`
	Headers         map[string]string `json:"headers"`
	Body            string            `json:"body"`
	IsBase64Encoded bool              `json:"isBase64Encoded"`
	Cookies         []string          `json:"cookies"`
}

var _ http.RoundTripper = (*Transport)(nil)

type Transport struct {
	lambda invokeAPIClient
}

func New(c *lambda.Client) *Transport {
	return &Transport{
		lambda: c,
	}
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	now := time.Now()
	r := &request{
		Version:         "2.0",
		RouteKey:        "$default",
		HTTPMethod:      req.Method,
		Body:            "",
		IsBase64Encoded: false,
		RawPath:         req.URL.RawPath,
		RawQueryString:  req.URL.RawQuery,
		Headers:         map[string]string{},
		Cookies:         []string{},
		RequestContext: &requestContext{
			HTTP: &requestContextHTTP{
				Method:    req.Method,
				Path:      req.URL.EscapedPath(),
				Protocol:  "HTTP/1.0",
				UserAgent: req.UserAgent(),
			},
			Time:      now.Format("02/Jan/2006:15:04:05 -0700"),
			TimeEpoch: now.UnixMilli(),
		},
	}

	payload, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}

	out, err := t.lambda.Invoke(ctx, &lambda.InvokeInput{
		FunctionName: aws.String(req.URL.Host),
		Payload:      payload,
	})
	if err != nil {
		return nil, err
	}

	var resp response
	if err := json.Unmarshal(out.Payload, &resp); err != nil {
		return nil, err
	}
	log.Println(string(out.Payload))

	return &http.Response{
		StatusCode: resp.StatusCode,
		Proto:      "HTTP/1.0",
		ProtoMajor: 1,
		ProtoMinor: 0,
		Body:       io.NopCloser(strings.NewReader(resp.Body)),
		Request:    req,
	}, nil
}
