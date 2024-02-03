package lambtrip

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
)

type Error struct {
	StatusCode int
	Payload    []byte
}

func (e *Error) Error() string {
	return fmt.Sprintf("unexpected status code %d", e.StatusCode)
}

const timeFormat = "02/Jan/2006:15:04:05 -0700"

var _ invokeAPIClient = (*lambda.Client)(nil)

type invokeAPIClient interface {
	Invoke(ctx context.Context, params *lambda.InvokeInput, optFns ...func(*lambda.Options)) (*lambda.InvokeOutput, error)
}

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

func (r *response) status() string {
	statusCode := r.statusCode()
	text := http.StatusText(statusCode)
	if text == "" {
		return strconv.Itoa(statusCode)
	}
	return strconv.Itoa(statusCode) + " " + text
}

func (r *response) statusCode() int {
	if r.StatusCode == 0 {
		return http.StatusOK
	}
	return r.StatusCode
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
	out, err := t.lambda.Invoke(ctx, &lambda.InvokeInput{
		FunctionName: aws.String(req.URL.Host),
		Payload:      payload,
	})
	if err != nil {
		return nil, err
	}

	if out.StatusCode != http.StatusOK {
		return nil, &Error{
			StatusCode: int(out.StatusCode),
			Payload:    out.Payload,
		}
	}

	// build the response
	var resp response
	if err := json.Unmarshal(out.Payload, &resp); err != nil {
		return nil, err
	}

	return &http.Response{
		Status:     resp.status(),
		StatusCode: resp.statusCode(),
		Proto:      "HTTP/1.0",
		ProtoMajor: 1,
		ProtoMinor: 0,
		Body:       io.NopCloser(strings.NewReader(resp.Body)),
		Request:    req,
	}, nil
}

func buildRequest(req *http.Request) (*request, error) {
	now := time.Now().UTC()

	// build the body
	isBase64Encoded := req.Body != nil && isBinary(req.Header.Get("Content-Type"))
	body := []byte{}
	if req.Body != nil {
		var err error
		body, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		if isBase64Encoded {
			buf := make([]byte, base64.StdEncoding.EncodedLen(len(body)))
			base64.StdEncoding.Encode(buf, body)
			body = buf
		}
	}

	// build the headers
	headers := make(map[string]string, len(req.Header))
	for k, v := range req.Header {
		if strings.EqualFold(k, "Cookie") {
			continue
		}
		headers[k] = strings.Join(v, ",")
	}

	// build the cookies
	cookies := make([]string, 0, len(req.Cookies()))
	for _, c := range req.Cookies() {
		cookies = append(cookies, c.String())
	}

	id, err := newRequestID()
	if err != nil {
		return nil, err
	}

	return &request{
		Version:         "2.0",
		RouteKey:        "$default",
		HTTPMethod:      req.Method,
		Body:            string(body),
		IsBase64Encoded: isBase64Encoded,
		RawPath:         req.URL.EscapedPath(),
		RawQueryString:  req.URL.RawQuery,
		Headers:         headers,
		Cookies:         cookies,
		RequestContext: &requestContext{
			RequestID: id,
			HTTP: &requestContextHTTP{
				Method:    req.Method,
				Path:      req.URL.Path,
				Protocol:  "HTTP/1.0",
				UserAgent: req.UserAgent(),
			},
			Time:      now.Format(timeFormat),
			TimeEpoch: now.UnixMilli(),
		},
	}, nil
}

// assume text/*, application/json, application/javascript, application/xml, */*+json, */*+xml as text
func isBinary(contentType string) bool {
	i := strings.Index(contentType, ";")
	if i == -1 {
		i = len(contentType)
	}
	mediaType := strings.TrimSpace(contentType[:i])
	i = strings.Index(mediaType, "/")
	if i == -1 {
		i = len(mediaType)
	}
	mainType := mediaType[:i]

	if strings.EqualFold(mainType, "text") {
		return false
	}
	if strings.EqualFold(mediaType, "application/json") {
		return false
	}
	if strings.EqualFold(mediaType, "application/javascript") {
		return false
	}
	if strings.EqualFold(mediaType, "application/xml") {
		return false
	}

	i = strings.LastIndex(mediaType, "+")
	if i == -1 {
		i = 0
	}
	suffix := mediaType[i:]
	if strings.EqualFold(suffix, "+json") {
		return false
	}
	if strings.EqualFold(suffix, "+xml") {
		return false
	}
	return true
}

func newRequestID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	buf[6] = (buf[6] & 0x0f) | 0x40 // set version to 4
	buf[8] = (buf[8] & 0x3f) | 0x80 // set variant to 10

	var dst [36]byte
	hex.Encode(dst[:], buf[:4])
	dst[8] = '-'
	hex.Encode(dst[9:], buf[4:6])
	dst[13] = '-'
	hex.Encode(dst[14:], buf[6:8])
	dst[18] = '-'
	hex.Encode(dst[19:], buf[8:10])
	dst[23] = '-'
	hex.Encode(dst[24:], buf[10:])
	return string(dst[:]), nil
}
