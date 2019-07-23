module gitlab.com/pantacor/pantahub-base

go 1.12

replace github.com/go-resty/resty => gopkg.in/resty.v1 v1.11.0

replace github.com/fundapps/go-json-rest-middleware-jwt => github.com/pantacor/go-json-rest-middleware-jwt v0.0.0-20190329232506-b7815ffda0af

replace github.com/ant0ine/go-json-rest => github.com/asac/go-json-rest v3.3.3-0.20181121222456-cab770813df3+incompatible

require (
	github.com/ChannelMeter/iso8601duration v0.0.0-20150204201828-8da3af7a2a61 // indirect
	github.com/alecthomas/units v0.0.0-20190717042225-c3de453c63f4
	github.com/ant0ine/go-json-rest v3.3.2+incompatible
	github.com/asaskevich/govalidator v0.0.0-20190424111038-f61b66f89f4a
	github.com/aws/aws-sdk-go v1.21.1
	github.com/bmizerany/assert v0.0.0-20160611221934-b7ed37b82869 // indirect
	github.com/channelmeter/iso8601duration v0.0.0-20150204201828-8da3af7a2a61 // indirect
	github.com/dgrijalva/jwt-go v3.2.0+incompatible
	github.com/dustinkirkland/golang-petname v0.0.0-20190613200456-11339a705ed2
	github.com/facebookgo/ensure v0.0.0-20160127193407-b4ab57deab51 // indirect
	github.com/facebookgo/stack v0.0.0-20160209184415-751773369052 // indirect
	github.com/facebookgo/subset v0.0.0-20150612182917-8dac2c3c4870 // indirect
	github.com/fatih/structs v1.1.0
	github.com/fluent/fluent-logger-golang v1.4.0
	github.com/fundapps/go-json-rest-middleware-jwt v0.0.0-00010101000000-000000000000
	github.com/gibson042/canonicaljson-go v1.0.3
	github.com/go-resty/resty v0.0.0-00010101000000-000000000000 // indirect
	github.com/go-stack/stack v1.8.0 // indirect
	github.com/golang/snappy v0.0.1 // indirect
	github.com/google/go-cmp v0.3.0 // indirect
	github.com/gorilla/mux v1.7.3 // indirect
	github.com/jaswdr/faker v1.0.2
	github.com/kr/pretty v0.1.0 // indirect
	github.com/miolini/datacounter v0.0.0-20171104152933-fd4e42a1d5e0 // indirect
	github.com/mongodb/mongo-go-driver v1.0.4
	github.com/onsi/ginkgo v1.8.0 // indirect
	github.com/onsi/gomega v1.5.0 // indirect
	github.com/pantacor/go-json-rest-middleware-jwt v0.0.0-20190329230644-1f6c0e03d26e
	github.com/philhofer/fwd v1.0.0 // indirect
	github.com/rs/cors v1.6.0
	github.com/stretchr/testify v1.3.0
	github.com/tidwall/pretty v1.0.0 // indirect
	github.com/tinylib/msgp v1.1.0 // indirect
	github.com/xdg/scram v0.0.0-20180814205039-7eeb5667e42c // indirect
	github.com/xdg/stringprep v1.0.0 // indirect
	gitlab.com/pantacor/pantahub-gc v0.0.0-20190719115544-466a41727898
	gitlab.com/pantacor/pantahub-testharness v0.0.0-20190311155708-e39aa76a7650
	gitlab.com/pantacor/pvr v0.0.0-20190722130419-325b73c63259
	go.mongodb.org/mongo-driver v1.0.4
	golang.org/x/crypto v0.0.0-20190701094942-4def268fd1a4
	golang.org/x/sync v0.0.0-20190423024810-112230192c58 // indirect
	gopkg.in/check.v1 v1.0.0-20180628173108-788fd7840127 // indirect
	gopkg.in/mailgun/mailgun-go.v1 v1.1.1
	gopkg.in/mgo.v2 v2.0.0-20180705113604-9856a29383ce
	gopkg.in/olivere/elastic.v5 v5.0.81
	gopkg.in/resty.v1 v1.12.0
	gopkg.in/yaml.v2 v2.2.2 // indirect
)
