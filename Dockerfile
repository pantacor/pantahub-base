FROM registry.gitlab.com/pantacor/pv-platforms/pantacmp:X86_64 as cmpossl
FROM golang:alpine3.9 as builder

ENV GO111MODULE=on

RUN apk add -U --no-cache \
    git \
    curl \
    build-base

WORKDIR /app/
COPY . .

RUN go get -d -v ./... \
    && go get github.com/swaggo/swag/cmd/swag && swag init \
    && go install -v ./...

FROM alpine
COPY --from=cmpossl --chown=0:0 /usr/local/ /usr/local

RUN apk update; apk add ca-certificates
COPY env.default /opt/ph/bin/
COPY --from=builder /go/bin/pantahub-base /opt/ph/bin/
COPY --from=builder /app/tmpl /opt/ph/bin/tmpl
COPY pantahub-base-docker-run /opt/ph/bin/
COPY localhost.cert.pem /opt/ph/bin/
COPY localhost.key.pem /opt/ph/bin/
EXPOSE 12365
EXPOSE 12366
CMD [ "/opt/ph/bin/pantahub-base-docker-run" ]
