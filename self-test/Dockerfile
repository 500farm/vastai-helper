FROM golang:alpine AS build-stage

WORKDIR /usr/local/go/src/build

COPY src/* go.mod go.sum ./
RUN go build -o /usr/local/bin/self-test .

FROM alpine

WORKDIR /usr/local/bin

COPY --from=build-stage /usr/local/bin/self-test ./

RUN apk --update add iproute2-minimal bind-tools iputils && \
    rm -rf /var/cache/apk/*

ENTRYPOINT ["/usr/local/bin/self-test"]
