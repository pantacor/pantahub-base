module gitlab.com/pantacor/pantahub-base

go 1.13

require (
	github.com/alecthomas/template v0.0.0-20190718012654-fb15b899a751
	github.com/alecthomas/units v0.0.0-20190924025748-f65c72e2690d
	github.com/ant0ine/go-json-rest v3.3.2+incompatible
	github.com/asaskevich/govalidator v0.0.0-20200907205600-7a23bdc65eef
	github.com/aws/aws-sdk-go v1.34.28
	github.com/cloudflare/cfssl v1.5.0
	github.com/dgrijalva/jwt-go v3.2.0+incompatible
	github.com/dustinkirkland/golang-petname v0.0.0-20191129215211-8e5a1ed0cff0
	github.com/fatih/structs v1.1.0
	github.com/fluent/fluent-logger-golang v1.5.0
	github.com/gibson042/canonicaljson-go v1.0.3
	github.com/gosimple/slug v1.9.0
	github.com/jaswdr/faker v1.2.1
	github.com/miolini/datacounter v1.0.2 // indirect
	github.com/pantacor/go-json-rest-middleware-jwt v0.0.0-20190329235955-213479ac018c
	github.com/prometheus/client_golang v1.8.0
	github.com/rs/cors v1.7.0
	github.com/stretchr/testify v1.6.1
	github.com/swaggo/http-swagger v0.0.0-20200308142732-58ac5e232fba
	github.com/swaggo/swag v1.6.9
	github.com/tiaguinho/gosoap v1.4.4
	github.com/tinylib/msgp v1.1.5 // indirect
	go.mongodb.org/mongo-driver v1.4.3
	golang.org/x/crypto v0.0.0-20201012173705-84dcc777aaee
	golang.org/x/oauth2 v0.0.0-20190226205417-e64efc72b421
	gopkg.in/mailgun/mailgun-go.v1 v1.1.1
	gopkg.in/mgo.v2 v2.0.0-20190816093944-a6b53ec6cb22
	gopkg.in/olivere/elastic.v5 v5.0.86
	gopkg.in/resty.v1 v1.12.0
	gopkg.in/square/go-jose.v2 v2.5.1
)

replace github.com/ant0ine/go-json-rest => github.com/asac/go-json-rest v3.3.3-0.20191004094541-40429adaafcb+incompatible
