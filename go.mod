module gitlab.com/pantacor/pantahub-base

go 1.12

replace github.com/go-resty/resty => gopkg.in/resty.v1 v1.11.0

replace github.com/ant0ine/go-json-rest => github.com/asac/go-json-rest v3.3.3-0.20191004094541-40429adaafcb+incompatible

require (
	github.com/ChannelMeter/iso8601duration v0.0.0-20150204201828-8da3af7a2a61 // indirect
	github.com/DataDog/zstd v1.4.4 // indirect
	github.com/alecthomas/template v0.0.0-20190718012654-fb15b899a751
	github.com/alecthomas/units v0.0.0-20190924025748-f65c72e2690d
	github.com/ant0ine/go-json-rest v3.3.2+incompatible
	github.com/asac/go-json-rest v3.3.2+incompatible
	github.com/asaskevich/govalidator v0.0.0-20200108200545-475eaeb16496
	github.com/aws/aws-sdk-go v1.29.21
	github.com/bmizerany/assert v0.0.0-20160611221934-b7ed37b82869 // indirect
	github.com/bombsimon/wsl/v2 v2.2.0 // indirect
	github.com/channelmeter/iso8601duration v0.0.0-20150204201828-8da3af7a2a61 // indirect
	github.com/cloudflare/cfssl v1.4.1
	github.com/coreos/etcd v3.3.18+incompatible // indirect
	github.com/coreos/go-systemd v0.0.0-20191104093116-d3cd4ed1dbcf // indirect
	github.com/dgrijalva/jwt-go v3.2.0+incompatible
	github.com/dustinkirkland/golang-petname v0.0.0-20191129215211-8e5a1ed0cff0
	github.com/emicklei/go-restful v2.11.2+incompatible
	github.com/facebookgo/ensure v0.0.0-20160127193407-b4ab57deab51 // indirect
	github.com/facebookgo/stack v0.0.0-20160209184415-751773369052 // indirect
	github.com/facebookgo/subset v0.0.0-20150612182917-8dac2c3c4870 // indirect
	github.com/fatih/color v1.9.0 // indirect
	github.com/fatih/structs v1.1.0
	github.com/fluent/fluent-logger-golang v1.5.0
	github.com/fsnotify/fsnotify v1.4.8 // indirect
	github.com/gibson042/canonicaljson-go v1.0.3
	github.com/go-chi/chi v4.0.2+incompatible
	github.com/go-openapi/spec v0.19.7 // indirect
	github.com/go-openapi/swag v0.19.8 // indirect
	github.com/go-resty/resty v0.0.0-00010101000000-000000000000 // indirect
	github.com/golang/groupcache v0.0.0-20200121045136-8c9f03a8e57e // indirect
	github.com/golang/mock v1.4.1 // indirect
	github.com/golangci/golangci-lint v1.23.8 // indirect
	github.com/google/certificate-transparency-go v1.1.0 // indirect
	github.com/google/monologue v0.0.0-20200310112848-e585696c5f1b // indirect
	github.com/gosimple/slug v1.9.0
	github.com/grpc-ecosystem/go-grpc-middleware v1.2.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway v1.14.3 // indirect
	github.com/jaswdr/faker v1.0.2
	github.com/jirfag/go-printf-func-name v0.0.0-20200119135958-7558a9eaa5af // indirect
	github.com/jmespath/go-jmespath v0.0.0-20200310193758-2437e8417af5 // indirect
	github.com/klauspost/compress v1.10.3 // indirect
	github.com/mattn/go-colorable v0.1.6 // indirect
	github.com/mattn/go-runewidth v0.0.8 // indirect
	github.com/miolini/datacounter v1.0.2 // indirect
	github.com/mongodb/mongo-go-driver v1.0.4
	github.com/olekukonko/tablewriter v0.0.4 // indirect
	github.com/pantacor/go-json-rest-middleware-jwt v0.0.0-20190329235955-213479ac018c
	github.com/philhofer/fwd v1.0.0 // indirect
	github.com/prometheus/client_golang v1.5.0
	github.com/rs/cors v1.7.0
	github.com/securego/gosec v0.0.0-20200302134848-c998389da2ac // indirect
	github.com/spf13/cast v1.3.1 // indirect
	github.com/spf13/cobra v0.0.6 // indirect
	github.com/spf13/viper v1.6.2 // indirect
	github.com/stretchr/testify v1.5.1
	github.com/swaggo/files v0.0.0-20190704085106-630677cd5c14
	github.com/swaggo/http-swagger v0.0.0-20200308142732-58ac5e232fba
	github.com/swaggo/swag v1.6.5
	github.com/tiaguinho/gosoap v1.4.3
	github.com/tinylib/msgp v1.1.2 // indirect
	github.com/tmc/grpc-websocket-proxy v0.0.0-20200122045848-3419fae592fc // indirect
	github.com/tommy-muehle/go-mnd v1.3.0 // indirect
	github.com/urfave/cli v1.22.3 // indirect
	gitlab.com/pantacor/pantahub-aca v0.0.0-20200304202411-2032737eb7a0
	gitlab.com/pantacor/pantahub-gc v0.0.0-20190719115544-466a41727898
	gitlab.com/pantacor/pantahub-testharness v0.0.0-20190311155708-e39aa76a7650
	go.etcd.io/etcd v3.3.18+incompatible // indirect
	go.mongodb.org/mongo-driver v1.3.1
	go.uber.org/multierr v1.5.0 // indirect
	go.uber.org/zap v1.14.0 // indirect
	golang.org/x/crypto v0.0.0-20200302210943-78000ba7a073
	golang.org/x/lint v0.0.0-20200302205851-738671d3881b // indirect
	golang.org/x/net v0.0.0-20200301022130-244492dfa37a // indirect
	golang.org/x/oauth2 v0.0.0-20200107190931-bf48bf16ab8d
	golang.org/x/sys v0.0.0-20200302150141-5c8b2ff67527 // indirect
	golang.org/x/tools v0.0.0-20200311090712-aafaee8bce8c // indirect
	google.golang.org/genproto v0.0.0-20200311144346-b662892dd51b // indirect
	google.golang.org/grpc v1.28.0 // indirect
	gopkg.in/ini.v1 v1.54.0 // indirect
	gopkg.in/mailgun/mailgun-go.v1 v1.1.1
	gopkg.in/mgo.v2 v2.0.0-20190816093944-a6b53ec6cb22
	gopkg.in/olivere/elastic.v5 v5.0.84
	gopkg.in/resty.v1 v1.12.0
	gopkg.in/square/go-jose.v2 v2.4.1
	honnef.co/go/tools v0.0.1-2020.1.3 // indirect
	sigs.k8s.io/yaml v1.2.0 // indirect
)
