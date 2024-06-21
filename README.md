# lambtrip

The package lambtrip is an adapter that converts APIs implemented in AWS Lambda into `http.RoundTripper`.
No need to use AWS Lambda Function URLs, AWS Gateway, ALB, etc.

## Synopsis

Here is a simple API implemented in AWS Lambda:

```javascript
"use strict";

exports.handler = async (event) => {
  // Lambda handler code
  return {
    body: `Hello World`,
    statusCode: 200,
  };
};
```

```yaml
AWSTemplateFormatVersion: "2010-09-09"
Transform: AWS::Serverless-2016-10-31
Description: Lambda Function URL
Resources:
  FURLFunction:
    Type: AWS::Serverless::Function
    Properties:
      CodeUri: src/
      Handler: app.handler
      Runtime: nodejs20.x
      Timeout: 3
      ### If you want to use Function URLs
      # FunctionUrlConfig:
      #   AuthType: AWS_IAM
      #   Cors:
      #     AllowOrigins: ["*"]

Outputs:
  FunctionURLEndpoint:
    Description: FURLFunction function name
    Value: !GetAtt FURLFunctionUrl.FunctionUrl
```

### Use as a library

You can call the API by following code:

```go
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
```

#### Specify the function qualifier

You can specify the function qualifier by the URL.

```go
resp, err := c.Get("lambda://xxx@function-name/foo/bar")
```

`xxx` is the function qualifier (alias name or version number).

### Use function-url-local command

function-url-local is a minimum clone of AWS Lambda Function URLs.

```
$ go install github.com/shogo82148/lambtrip/cmd/function-url-local@latest
$ function-url-local function-name
{"time":"2024-02-05T22:28:57.781792+09:00","level":"INFO","msg":"starting the server","addr":":8080"}
```
