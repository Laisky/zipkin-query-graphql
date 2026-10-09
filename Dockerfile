FROM golang:1.27.2-alpine3.23@sha256:2ac5c2a64f1f970b5120fe21c6a5e3d9190b196a9ead95797564738ecd07a8a2 AS gobuild

# run dependencies
RUN apk update && apk upgrade && \
    apk add --no-cache gcc git build-base ca-certificates curl && \
    update-ca-certificates

ENV GO111MODULE=on
WORKDIR /goapp

COPY go.mod .
COPY go.sum .
RUN go mod download

# static build
ADD . .
RUN go build -a -tags timetzdata --ldflags '-linkmode external -extldflags "-static"' entrypoints/main.go


# copy executable file and certs to a pure container
FROM alpine:3.9
COPY --from=gobuild /etc/ssl/certs /etc/ssl/certs
COPY --from=gobuild /goapp/main go-zipkin-query

ENTRYPOINT [ "./go-zipkin-query" ]
CMD ["--debug", "--addr=0.0.0.0:8090"]
