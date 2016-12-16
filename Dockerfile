FROM golang:1.6

RUN mkdir -p /go/src/pantahub-base
WORKDIR /go/src/pantahub-base

# this will ideally be built by the ONBUILD below ;)
CMD ["go-wrapper", "run"]

COPY . /go/src/pantahub-base
RUN go-wrapper download
RUN go-wrapper install

EXPOSE 12365

