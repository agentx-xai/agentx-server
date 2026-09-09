FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/agentx-api ./cmd/app
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/agentx-migrate ./cmd/migrate
FROM alpine:3.22
RUN apk upgrade --no-cache && addgroup -S agentx && adduser -S -G agentx agentx
WORKDIR /app
COPY --from=build /out/agentx-api /app/agentx-api
COPY --from=build /out/agentx-migrate /app/agentx-migrate
COPY migrations /app/migrations
RUN mkdir -p /var/lib/agentx && chown -R agentx:agentx /var/lib/agentx
USER agentx
ENV AGENTX_DATA_DIR=/var/lib/agentx
EXPOSE 8080
ENTRYPOINT ["/app/agentx-api"]
