module gitlab.com/pantacor/pantahub-base

go 1.13

require (
	bitbucket.org/creachadair/shell v0.0.7 // indirect
	cloud.google.com/go/compute v1.5.0 // indirect
	github.com/alecthomas/template v0.0.0-20190718012654-fb15b899a751
	github.com/alecthomas/units v0.0.0-20211218093645-b94a6e3cc137
	github.com/ant0ine/go-json-rest v3.3.2+incompatible
	github.com/asaskevich/govalidator v0.0.0-20210307081110-f21760c49a8d
	github.com/aws/aws-sdk-go v1.43.34
	github.com/bmizerany/assert v0.0.0-20160611221934-b7ed37b82869 // indirect
	github.com/cloudflare/cfssl v1.6.1
	github.com/cncf/udpa/go v0.0.0-20220112060539-c52dc94e7fbe // indirect
	github.com/cncf/xds/go v0.0.0-20220330162227-eded343319d0 // indirect
	github.com/dgrijalva/jwt-go v3.2.0+incompatible
	github.com/dustinkirkland/golang-petname v0.0.0-20191129215211-8e5a1ed0cff0
	github.com/envoyproxy/protoc-gen-validate v0.6.7 // indirect
	github.com/facebookgo/ensure v0.0.0-20200202191622-63f1cf65ac4c // indirect
	github.com/fatih/structs v1.1.0
	github.com/fluent/fluent-logger-golang v1.9.0
	github.com/form3tech-oss/jwt-go v3.2.5+incompatible // indirect
	github.com/fullstorydev/grpcurl v1.8.6 // indirect
	github.com/gibson042/canonicaljson-go v1.0.3
	github.com/go-openapi/swag v0.21.1 // indirect
	github.com/golang/snappy v0.0.4 // indirect
	github.com/google/certificate-transparency-go v1.1.2 // indirect
	github.com/google/go-cmp v0.5.9 // indirect
	github.com/gorilla/websocket v1.5.0 // indirect
	github.com/gosimple/slug v1.12.0
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.10.0 // indirect
	github.com/jaswdr/faker v1.2.1
	github.com/jhump/protoreflect v1.12.0 // indirect
	github.com/jonboulle/clockwork v0.2.3 // indirect
	github.com/klauspost/compress v1.15.1 // indirect
	github.com/mattn/go-colorable v0.1.11 // indirect
	github.com/mattn/go-runewidth v0.0.13 // indirect
	github.com/olivere/elastic/v7 v7.0.32
	github.com/pantacor/go-json-rest-middleware-jwt v0.0.0-20190329235955-213479ac018c
	github.com/prometheus/client_golang v1.12.1
	github.com/prometheus/common v0.33.0 // indirect
	github.com/rs/cors v1.8.2
	github.com/spf13/cobra v1.4.0 // indirect
	github.com/stretchr/testify v1.8.0
	github.com/swaggo/http-swagger v1.2.5
	github.com/swaggo/swag v1.8.1
	github.com/tiaguinho/gosoap v1.4.4
	github.com/tinylib/msgp v1.1.6 // indirect
	github.com/tmc/grpc-websocket-proxy v0.0.0-20220101234140-673ab2c3ae75 // indirect
	github.com/youmark/pkcs8 v0.0.0-20201027041543-1326539a0a0a // indirect
	gitlab.com/pantacor/pantahub-gc v0.0.0-20220111192912-df394e800210
	gitlab.com/pantacor/pantahub-testharness v0.0.0-20190311155708-e39aa76a7650
	go.etcd.io/etcd/v3 v3.5.2 // indirect
	go.mongodb.org/mongo-driver v1.10.2
	go.opentelemetry.io/contrib/instrumentation/go.mongodb.org/mongo-driver/mongo/otelmongo v0.36.0
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.31.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.36.0
	go.opentelemetry.io/otel v1.10.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.10.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.10.0
	go.opentelemetry.io/otel/sdk v1.10.0
	go.opentelemetry.io/otel/sdk/export/metric v0.28.0 // indirect
	go.opentelemetry.io/otel/trace v1.10.0
	go.uber.org/atomic v1.9.0 // indirect
	go.uber.org/multierr v1.8.0 // indirect
	go.uber.org/zap v1.21.0 // indirect
	golang.org/x/crypto v0.0.0-20220622213112-05595931fe9d
	golang.org/x/net v0.7.0 // indirect
	golang.org/x/oauth2 v0.0.0-20220309155454-6242fa91716a
	golang.org/x/time v0.0.0-20220224211638-0e9765cccd65 // indirect
	google.golang.org/genproto v0.0.0-20220407144326-9054f6ed7bac // indirect
	google.golang.org/grpc v1.49.0
	gopkg.in/mailgun/mailgun-go.v1 v1.1.1
	gopkg.in/mgo.v2 v2.0.0-20190816093944-a6b53ec6cb22
	gopkg.in/resty.v1 v1.12.0
	gopkg.in/square/go-jose.v2 v2.6.0
)

replace github.com/ant0ine/go-json-rest => github.com/pantacor/go-json-rest v0.0.0-20220930143134-28fc15ec4ffc

replace github.com/tiaguinho/gosoap => github.com/highercomve/gosoap v1.3.0-a747454
