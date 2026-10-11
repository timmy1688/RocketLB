FROM node:20-bookworm-slim AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM golang:1.24-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -o /out/rocketlb ./cmd/rocketlb

FROM debian:bookworm-slim
RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates \
  && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=build /out/rocketlb /usr/local/bin/rocketlb
COPY --from=build /src/web/dist /app/web/dist
ENV ROCKETLB_HTTP_ADDR=:8080 \
    ROCKETLB_ENV=production \
    ROCKETLB_DATA_DIR=/data \
    ROCKETLB_DB_DRIVER=postgres \
    ROCKETLB_DB_DSN=postgres://rocketlb:rocketlb@postgres:5432/rocketlb?sslmode=disable \
    ROCKETLB_WEB_DIST=/app/web/dist
EXPOSE 8080
ENTRYPOINT ["rocketlb"]
