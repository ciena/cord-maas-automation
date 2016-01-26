FROM golang

RUN go get github.com/tools/godep

ADD Godeps Godeps
RUN godep restore

RUN go install github.com/ciena/maas-flow

ENTRYPOINT ["/go/bin/maas-flow"]
