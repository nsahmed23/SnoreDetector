FROM golang:1.25-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
    -o /out/export-service ./cmd/export-service

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/export-service /usr/local/bin/export-service
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
USER nonroot:nonroot
EXPOSE 8082
ENTRYPOINT ["/usr/local/bin/export-service"]
