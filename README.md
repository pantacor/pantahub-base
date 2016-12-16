
## native

TO build raw:

1. install go
2. create a workspace (mkdir workspace)
3. set GOPATH to that directory (e.g. GOPATH=$PWD/workspace)
4. mkdir src/
5. clone this pantahub-base repo to src/
   git clone ... src/
6. cd src/pantahub-base
7. go get
8. go build


## docker

TO build docker:

docker build -t pantahub-base .

TO run in docker:
docker run --rm --env-file docker.env -it --name pantahub-base pantahub-serv pantahub-serv

## mongo setup for docker access

configure mongo to listen on iface for docker:
check out what IP docker0 interface has for you and add it to the comma
separated list in /etc/mongodb.conf bind_ip field, like:

bind_ip = 127.0.0.1,172.17.0.1

