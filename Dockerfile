FROM golang:1.24-alpine AS development

RUN apk add --no-cache ca-certificates git \
    && addgroup -S bot \
    && adduser -S -G bot -h /app bot \
    && go install github.com/air-verse/air@v1.61.7

WORKDIR /app
RUN mkdir -p /go/pkg/mod /home/bot/.cache/go-build \
    && chown -R bot:bot /app /go/pkg/mod /home/bot
USER bot

COPY --chown=bot:bot go.mod go.sum* ./
RUN go mod download

COPY --chown=bot:bot . .
CMD ["air", "-c", ".air.toml"]

FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/bot ./cmd/bot

FROM alpine:3.21 AS production
RUN apk add --no-cache ca-certificates \
    && addgroup -S bot \
    && adduser -S -G bot bot
COPY --from=build /out/bot /usr/local/bin/bot
USER bot
ENTRYPOINT ["/usr/local/bin/bot"]
