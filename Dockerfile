FROM golang:1.12.6-alpine3.9 AS gobuild

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
RUN go build -a --ldflags '-extldflags "-static"' entrypoints/main.go


# copy executable file and certs to a pure container
FROM alpine:3.9
COPY --from=gobuild /etc/ssl/certs /etc/ssl/certs
COPY --from=gobuild /goapp/main go-zipkin-query

ENTRYPOINT [ "./go-zipkin-query" ]
CMD ["--debug", "--addr=0.0.0.0:8090"]
