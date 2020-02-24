module gitlab.com/pantacor/pantahub-base

go 1.12

replace github.com/go-resty/resty => gopkg.in/resty.v1 v1.11.0

replace github.com/ant0ine/go-json-rest => github.com/asac/go-json-rest v3.3.3-0.20191004094541-40429adaafcb+incompatible

replace github.com/tiaguinho/gosoap => github.com/highercomve/gosoap v1.3.0-a747454

require (
	github.com/ChannelMeter/iso8601duration v0.0.0-20150204201828-8da3af7a2a61 // indirect
	github.com/DataDog/zstd v1.4.4 // indirect
	github.com/alecthomas/template v0.0.0-20190718012654-fb15b899a751
	github.com/alecthomas/units v0.0.0-20190924025748-f65c72e2690d
	github.com/ant0ine/go-json-rest v3.3.2+incompatible
	github.com/asac/go-json-rest v3.3.2+incompatible
	github.com/asaskevich/govalidator v0.0.0-20200108200545-475eaeb16496
	github.com/aws/aws-sdk-go v1.28.14
	github.com/bmizerany/assert v0.0.0-20160611221934-b7ed37b82869 // indirect
	github.com/channelmeter/iso8601duration v0.0.0-20150204201828-8da3af7a2a61 // indirect
	github.com/cloudflare/cfssl v1.4.1
	github.com/dgrijalva/jwt-go v3.2.0+incompatible
	github.com/dustinkirkland/golang-petname v0.0.0-20191129215211-8e5a1ed0cff0
	github.com/emicklei/go-restful v2.10.0+incompatible // indirect
	github.com/emicklei/go-restful-openapi v1.2.0 // indirect
	github.com/facebookgo/ensure v0.0.0-20160127193407-b4ab57deab51 // indirect
	github.com/facebookgo/stack v0.0.0-20160209184415-751773369052 // indirect
	github.com/facebookgo/subset v0.0.0-20150612182917-8dac2c3c4870 // indirect
	github.com/fatih/structs v1.1.0
	github.com/fluent/fluent-logger-golang v1.4.0
	github.com/gibson042/canonicaljson-go v1.0.3
	github.com/go-chi/chi v4.0.2+incompatible
	github.com/go-openapi/spec v0.19.6 // indirect
	github.com/go-openapi/swag v0.19.7 // indirect
	github.com/go-resty/resty v0.0.0-00010101000000-000000000000 // indirect
	github.com/golang/protobuf v1.3.3 // indirect
	github.com/gosimple/slug v1.9.0
	github.com/jaswdr/faker v1.0.2
	github.com/klauspost/compress v1.10.0 // indirect
	github.com/mailru/easyjson v0.7.0 // indirect
	github.com/miolini/datacounter v1.0.2 // indirect
	github.com/mongodb/mongo-go-driver v1.0.4
	github.com/onsi/ginkgo v1.8.0 // indirect
	github.com/onsi/gomega v1.5.0 // indirect
	github.com/pantacor/go-json-rest-middleware-jwt v0.0.0-20190329235955-213479ac018c
	github.com/philhofer/fwd v1.0.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/prometheus/client_golang v1.4.1
	github.com/rs/cors v1.7.0
	github.com/stretchr/testify v1.4.0
	github.com/swaggo/files v0.0.0-20190704085106-630677cd5c14
	github.com/swaggo/http-swagger v0.0.0-20200103000832-0e9263c4b516
	github.com/swaggo/swag v1.6.5
	github.com/tiaguinho/gosoap v1.2.0
	github.com/tinylib/msgp v1.1.1 // indirect
	github.com/xdg/stringprep v1.0.0 // indirect
	gitlab.com/pantacor/pantahub-gc v0.0.0-20190719115544-466a41727898
	gitlab.com/pantacor/pantahub-testharness v0.0.0-20190311155708-e39aa76a7650
	go.mongodb.org/mongo-driver v1.3.0
	golang.org/x/crypto v0.0.0-20200210222208-86ce3cb69678
	golang.org/x/lint v0.0.0-20191125180803-fdd1cda4f05f // indirect
	golang.org/x/net v0.0.0-20200202094626-16171245cfb2 // indirect
	golang.org/x/sys v0.0.0-20200202164722-d101bd2416d5 // indirect
	golang.org/x/tools v0.0.0-20200211045251-2de505fc5306 // indirect
	gopkg.in/mailgun/mailgun-go.v1 v1.1.1
	gopkg.in/mgo.v2 v2.0.0-20190816093944-a6b53ec6cb22
	gopkg.in/olivere/elastic.v5 v5.0.84
	gopkg.in/resty.v1 v1.12.0
	gopkg.in/square/go-jose.v2 v2.4.1
	gopkg.in/yaml.v2 v2.2.8 // indirect
)
