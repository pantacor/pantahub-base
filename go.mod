module pantahub-base

go 1.12

require (
	github.com/alecthomas/units v0.0.0-20151022065526-2efee857e7cf
	github.com/ant0ine/go-json-rest v3.3.2+incompatible // indirect
	github.com/asaskevich/govalidator v0.0.0-20170903095215-73945b6115bf
	github.com/aws/aws-sdk-go v1.12.79
	github.com/dgrijalva/jwt-go v0.0.0-20160705203006-01aeca54ebda
	github.com/dustinkirkland/golang-petname v0.0.0-20170921220637-d3c2ba80e75e
	github.com/fatih/structs v0.0.0-20181010231757-878a968ab225
	github.com/fluent/fluent-logger-golang v1.4.0
	github.com/fundapps/go-json-rest-middleware-jwt v0.0.0-00010101000000-000000000000 // indirect
	github.com/gibson042/canonicaljson-go v0.0.0-20171116213509-53c2489e9cef
	github.com/go-ini/ini v0.0.0-20190217195415-ece0e89bb05a
	github.com/go-resty/resty v0.0.0-20170925192930-9ac9c42358f7
	github.com/go-stack/stack v1.8.0
	github.com/golang/snappy v0.0.1
	github.com/jmespath/go-jmespath v0.0.0-20180206201540-c2b33e8439af
	github.com/mailru/easyjson v0.0.0-20190221075403-6243d8e04c3f
	github.com/miolini/datacounter v0.0.0-20171104152933-fd4e42a1d5e0
	github.com/mongodb/mongo-go-driver v1.0.0-rc1
	github.com/philhofer/fwd v1.0.0
	github.com/pkg/errors v0.8.0
	github.com/rs/cors v0.0.0-20190116175910-76f58f330d76
	github.com/stretchr/testify v1.3.0
	github.com/tinylib/msgp v0.0.0-20190103190839-ade0ca4ace05
	github.com/xdg/scram v0.0.0-20180814205039-7eeb5667e42c
	github.com/xdg/stringprep v0.0.0-20180714160509-73f8eece6fdc
	gitlab.com/pantacor/pantahub-base v0.0.0-20190716191021-0d5844a86900
	gitlab.com/pantacor/pvr v0.0.0-20170930172455-16997ebde0fb
	gitlab.com/pantacor/pvr.git v0.0.0-20170930172455-16997ebde0fb
	go.mongodb.org/mongo-driver v1.0.3
	golang.org/x/crypto v0.0.0-20190228161510-8dd112bcdc25
	golang.org/x/net v0.0.0-20190301231341-16b79f2e4e95
	golang.org/x/sync v0.0.0-20190227155943-e225da77a7e6
	golang.org/x/text v0.0.0-20190306152657-5d731a35f486
	gopkg.in/mailgun/mailgun-go.v1 v1.1.1
	gopkg.in/mgo.v2 v2.0.0-20180705113604-9856a29383ce
	gopkg.in/olivere/elastic.v5 v5.0.81
)

replace (
	github.com/ant0ine/go-json-rest => github.com/asac/go-json-rest v3.3.3-0.20181121222456-cab770813df3+incompatible
	github.com/fundapps/go-json-rest-middleware-jwt => github.com/pantacor/go-json-rest-middleware-jwt v0.0.0-20190329235955-213479ac018c
)
