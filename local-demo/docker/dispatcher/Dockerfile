FROM golang:1.25 AS build

WORKDIR /app

COPY . /app
RUN cd /app/src/ && make build-release

FROM alpine:edge

WORKDIR /app
COPY --from=build /app/src/build/release/ocf-amd64 .
RUN apk --no-cache add ca-certificates tzdata

ENTRYPOINT ["/app/ocf-amd64"]