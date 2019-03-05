FROM golang:alpine as builder

WORKDIR /go/src/gitlab.com/pantacor/pantahub-gc
COPY . .

RUN apk update; apk add git ca-certificates --no-cache
RUN go get -d -v ./...
RUN go install -v ./...

FROM alpine

COPY env.default /opt/ph/bin/
COPY --from=builder /go/bin/pantahub-gc /opt/ph/bin/
COPY pantahub-gc-docker-run /opt/ph/bin/
COPY localhost.cert.pem /opt/ph/bin/
COPY localhost.key.pem /opt/ph/bin/
EXPOSE 2000
EXPOSE 2001

CMD [ "/opt/ph/bin/pantahub-gc-docker-run" ]

