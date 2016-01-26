FROM golang

#ADD maas-flow.go /go/src/github.com/ciena/maas-flow/maas-flow.go
#ADD state.go /go/src/github.com/ciena/maas-flow/state.go
#ADD node.go /go/src/github.com/ciena/maas-flow/node.go
#ADD src/github.com/juju/gomaasapi /go/src/github.com/juju/gomassapi

ADD src/github.com /go/src/github.com
ADD src/gopkg.in /go/src/gopkg.in

RUN ls -R

RUN go install github.com/ciena/maas-flow

ENTRYPOINT ["/go/bin/maas-flow"]
