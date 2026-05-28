FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o server .

FROM gcr.io/distroless/static-debian12
WORKDIR /app
COPY --from=builder /app/server .
ENTRYPOINT ["/app/server"]
CMD ["server", "-c", "/config/config.yaml"]
