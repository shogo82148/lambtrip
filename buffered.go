package lambtrip

import (
	"bytes"
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

// LambdaError is an error returned by the lambda client.
type LambdaError struct {
	StatusCode int
	Payload    []byte
}

func (e *LambdaError) Error() string {
	return fmt.Sprintf("lambtrip: unexpected status code %d", e.StatusCode)
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

func (r *response) header() http.Header {
	h := make(http.Header, len(r.Headers)+len(r.Cookies))
	for k, v := range r.Headers {
		h.Set(k, v)
	}

	for _, c := range r.Cookies {
		h.Add("Set-Cookie", c)
	}

	if ct := h.Get("Content-Type"); ct == "" {
		h.Set("Content-Type", "application/json")
	}
	return h
}

func (r *response) body() (body io.ReadCloser, contentLength int64, err error) {
	if r.IsBase64Encoded {
		decoded, err := base64.StdEncoding.DecodeString(r.Body)
		if err != nil {
			return nil, 0, err
		}
		reader := bytes.NewReader(decoded)
		return io.NopCloser(reader), int64(len(decoded)), nil
	}

	reader := strings.NewReader(r.Body)
	return io.NopCloser(reader), int64(len(r.Body)), nil
}

var _ http.RoundTripper = (*BufferedTransport)(nil)

type BufferedTransport struct {
	lambda invokeAPIClient
}

func NewBufferedTransport(c *lambda.Client) *BufferedTransport {
	return &BufferedTransport{
		lambda: c,
	}
}

func (t *BufferedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
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
	in := &lambda.InvokeInput{
		FunctionName: aws.String(req.URL.Host),
		Payload:      payload,
	}
	if req.URL.User != nil {
		// lambda://alias@function
		in.Qualifier = aws.String(req.URL.User.Username())
	}
	out, err := t.lambda.Invoke(ctx, in)
	if err != nil {
		return nil, err
	}

	if out.StatusCode != http.StatusOK {
		return nil, &LambdaError{
			StatusCode: int(out.StatusCode),
			Payload:    out.Payload,
		}
	}

	// build the response
	var resp response
	if err := json.Unmarshal(out.Payload, &resp); err != nil {
		return nil, err
	}
	return buildResponse(&resp, req)
}

func buildRequest(req *http.Request) (*request, error) {
	now := time.Now().UTC()

	// build the body
	isBase64Encoded := req.Body != nil && isBinary(req.Header)
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

// assume text/*, application/json, application/javascript, application/xml, */*+json, */*+xml, etc. as text
func isBinary(headers http.Header) bool {
	contentEncoding := headers.Values("Content-Encoding")
	if len(contentEncoding) > 0 {
		// typically, gzip, deflate, br, etc.
		// these compressed encodings are not text, they are binary.
		return true
	}

	contentType := headers.Get("Content-Type")
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

	// common text mime types
	if strings.EqualFold(mainType, "text") {
		return false
	}
	if strings.EqualFold(mediaType, "application/json") {
		return false
	}
	if strings.EqualFold(mediaType, "application/yaml") {
		return false
	}
	if strings.EqualFold(mediaType, "application/javascript") {
		return false
	}
	if strings.EqualFold(mediaType, "application/xml") {
		return false
	}

	// custom text mime types, such as application/*+json, application/*+xml
	i = strings.LastIndex(mediaType, "+")
	if i == -1 {
		i = 0
	}
	suffix := mediaType[i:]
	if strings.EqualFold(suffix, "+json") {
		return false
	}
	if strings.EqualFold(suffix, "+yaml") {
		return false
	}
	if strings.EqualFold(suffix, "+xml") {
		return false
	}

	// assume it's binary
	return true
}

func buildResponse(resp *response, req *http.Request) (*http.Response, error) {
	h := resp.header()
	body, length, err := resp.body()
	if err != nil {
		return nil, fmt.Errorf("lambtrip: failed to build response body: %w", err)
	}
	h.Set("Content-Length", strconv.FormatInt(length, 10))
	return &http.Response{
		Status:        resp.status(),
		StatusCode:    resp.statusCode(),
		Proto:         "HTTP/1.0",
		ProtoMajor:    1,
		ProtoMinor:    0,
		Request:       req,
		Header:        h,
		ContentLength: length,
		Body:          body,
		Close:         true,
	}, nil
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
