FROM golang:alpine as builder

WORKDIR /go/src/gitlab.com/pantacor/pantahub-base
COPY . .

RUN apk update; apk add git curl
RUN sh -c "curl https://glide.sh/get | sh"; glide install -v
RUN go install -v ./...

FROM alpine

COPY env.default /opt/ph/bin/
COPY --from=builder /go/bin/pantahub-base /opt/ph/bin/
COPY pantahub-base-docker-run /opt/ph/bin/
COPY localhost.cert.pem /opt/ph/bin/
COPY localhost.key.pem /opt/ph/bin/
EXPOSE 12365
EXPOSE 12366

CMD [ "/opt/ph/bin/pantahub-base-docker-run" ]

RUN apk update; apk add ca-certificates

