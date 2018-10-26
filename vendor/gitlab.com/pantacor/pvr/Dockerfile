FROM golang:alpine as src

WORKDIR /go/src/gitlab.com/pantacor/pvr
COPY . .

RUN apk update; apk add git
RUN version=`git describe --tags` && sed -i "s/NA/$version/" version.go

# build amd64 linux static
FROM golang:alpine as linux_amd64

WORKDIR /go/src/gitlab.com/pantacor/pvr
COPY --from=src /go/src/gitlab.com/pantacor/pvr .
RUN apk update; apk add git
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go get -d -v ./...
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /go/bin/linux_amd64/pvr -v .

# build armv6 linux static
FROM golang:alpine as linux_armv6

WORKDIR /go/src/gitlab.com/pantacor/pvr
COPY --from=src /go/src/gitlab.com/pantacor/pvr .
RUN apk update; apk add git
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=5 go get -d -v ./...
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=5 go build -o /go/bin/linux_armv6/pvr -v .

# build windows i386 static
FROM golang:alpine as windows_386

WORKDIR /go/src/gitlab.com/pantacor/pvr
COPY --from=src /go/src/gitlab.com/pantacor/pvr .
RUN apk update; apk add git
RUN CGO_ENABLED=0 GOOS=windows GOARCH=386 go get -d -v ./...
RUN CGO_ENABLED=0 GOOS=windows GOARCH=386 go build -o /go/bin/windows_386/pvr -v .

# build windows amd64 static
FROM golang:alpine as windows_amd64

WORKDIR /go/src/gitlab.com/pantacor/pvr
COPY --from=src /go/src/gitlab.com/pantacor/pvr .
RUN apk update; apk add git
RUN CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go get -d -v ./...
RUN CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o /go/bin/windows_amd64/pvr -v .

FROM alpine

RUN apk update && apk add ca-certificates && rm -rf /var/cache/apk/*

WORKDIR /work
COPY --from=linux_amd64 /go/bin /pkg/bin
COPY --from=linux_armv6 /go/bin /pkg/bin
COPY --from=windows_386 /go/bin /pkg/bin
COPY --from=windows_amd64 /go/bin /pkg/bin

ENV USER root

ENTRYPOINT [ "/bin/tar", "-C", "/pkg/", "-c", "." ]

