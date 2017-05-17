FROM golang

WORKDIR /go/src/pantahub-base
ADD . .
RUN go-wrapper download
RUN go-wrapper install
ENTRYPOINT /go/bin/pantahub-base

EXPOSE 12365
EXPOSE 12366

VOLUME ["/var/pantahub/local-s3", "/var/log/pantahub"]
