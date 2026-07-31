FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o heron-ops . \
 && ln -s heron-ops steadyhelm

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /app/heron-ops /heron-ops
COPY --from=builder /app/steadyhelm /steadyhelm
USER nonroot:nonroot
ENTRYPOINT ["/heron-ops"]
