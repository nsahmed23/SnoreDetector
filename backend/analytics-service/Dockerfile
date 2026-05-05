FROM golang:1.25-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
    -o /out/analytics-service ./cmd/analytics-service

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/analytics-service /usr/local/bin/analytics-service
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
USER nonroot:nonroot
EXPOSE 8081
ENTRYPOINT ["/usr/local/bin/analytics-service"]
