FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /pingu .

FROM alpine:3.22
RUN apk add --no-cache ca-certificates && \
    addgroup -g 10001 pingu && adduser -D -u 10001 -G pingu pingu && \
    mkdir /data && chown pingu:pingu /data
COPY --from=build /pingu /usr/local/bin/pingu
USER 10001:10001
ENV PINGU_ADDR=0.0.0.0:8080 PINGU_DATA=/data/jobs.json GOMEMLIMIT=96MiB
EXPOSE 8080
ENTRYPOINT ["pingu"]
