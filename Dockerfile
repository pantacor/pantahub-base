FROM golang:alpine3.9 as builder

ENV GO111MODULE=on

WORKDIR /app/
COPY . .

RUN apk update; apk add git curl
RUN go install -v .

FROM alpine

COPY env.default /opt/ph/bin/
COPY --from=builder /go/bin/pantahub-base /opt/ph/bin/
COPY --from=builder /app/tmpl /opt/ph/bin/tmpl
COPY pantahub-base-docker-run /opt/ph/bin/
COPY localhost.cert.pem /opt/ph/bin/
COPY localhost.key.pem /opt/ph/bin/
EXPOSE 12365
EXPOSE 12366

CMD [ "/opt/ph/bin/pantahub-base-docker-run" ]

RUN apk update; apk add ca-certificates

