FROM golang:1.22.6-alpine AS builder

ENV GO111MODULE=on

RUN apk add -U --no-cache \
	git \
	curl \
	build-base

WORKDIR /app/
COPY . .

RUN go get -v ./... \
	&& go install github.com/swaggo/swag/cmd/swag@v1.6.9 && swag init \
	&& go install -v ./...

FROM alpine

RUN apk update; apk add ca-certificates
COPY env.default /opt/ph/bin/
COPY --from=builder /go/bin/pantahub-base /opt/ph/bin/
COPY --from=builder /app/tmpl /opt/ph/bin/tmpl
COPY entrypoints/pantahub-base-docker-run /opt/ph/bin/
COPY localhost.cert.pem /opt/ph/bin/
COPY localhost.key.pem /opt/ph/bin/
EXPOSE 12365
EXPOSE 12366
CMD [ "/opt/ph/bin/pantahub-base-docker-run" ]
