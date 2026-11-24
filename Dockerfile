FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/euryphos-server ./cmd/euryphos-server

FROM alpine:3.21
RUN addgroup -S euryphos && adduser -S -G euryphos euryphos \
    && mkdir -p /var/lib/euryphos \
    && chown -R euryphos:euryphos /var/lib/euryphos
WORKDIR /app
COPY --from=build /out/euryphos-server /usr/local/bin/euryphos-server
USER euryphos
VOLUME ["/var/lib/euryphos"]
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/euryphos-server"]
