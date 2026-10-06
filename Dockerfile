FROM golang:1.27.1-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /ghostview ./cmd/server

FROM alpine:3.23
RUN apk add --no-cache ca-certificates && addgroup -S ghostview && adduser -S -G ghostview ghostview
WORKDIR /app
COPY --from=build /ghostview /app/ghostview
COPY web /app/web
USER ghostview
EXPOSE 8080
ENV ADDR=:8080 WEB_DIR=/app/web PROVIDER_MODE=live
HEALTHCHECK --interval=30s --timeout=3s CMD wget -q -O /dev/null http://127.0.0.1:8080/api/v1/health || exit 1
ENTRYPOINT ["/app/ghostview"]
