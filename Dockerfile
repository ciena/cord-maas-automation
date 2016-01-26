FROM golang

ADD src /go/src/github.com/golang

RUN go install github.com/ciena/maas-flow

ENTRYPOINT /got/bin/maas-flow
