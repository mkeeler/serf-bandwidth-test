FROM golang:latest as builder

RUN mkdir /serf-bandwidth-test
ADD . /serf-bandwidth-test/

WORKDIR /serf-bandwidth-test
RUN CGO_ENABLED=0 go build

FROM alpine:latest

RUN apk add --no-cache iotop

COPY --from=builder /serf-bandwidth-test/serf-bandwidth-test /usr/bin/serf-bandwidth-test

ENTRYPOINT ["/usr/bin/serf-bandwidth-test"]
CMD ["-help"]