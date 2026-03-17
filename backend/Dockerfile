FROM golang:1.26-alpine AS builder

WORKDIR /app

ENV GOPRIVATE=github.com/clear-route/*
ENV GONOSUMDB=github.com/clear-route/*
ENV GONOPROXY=github.com/clear-route/*

COPY go.mod go.sum ./

# TODO later use .netrc and docker secrets
RUN go mod download && go mod verify 

COPY . .

RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -o orbit-backend ./cmd/orbit/main.go

FROM alpine:3.23
WORKDIR /app

RUN apk add --no-cache ca-certificates

COPY ./cmd/orbit/config/*.yaml ./cmd/orbit/config/
COPY ./internal/platform/storage/config/*.yaml ./internal/platform/storage/config/
COPY ./internal/identity/config/*.yaml ./internal/identity/config/

COPY --from=builder /app/orbit-backend .

EXPOSE 8080
CMD ["./orbit-backend"]

