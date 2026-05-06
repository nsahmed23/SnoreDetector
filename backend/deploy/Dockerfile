FROM golang:1.25-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
    -o /out/sync-service ./cmd/sync-service

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/sync-service /usr/local/bin/sync-service
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/sync-service"]
