FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.23-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./internal/httpapi/web
RUN CGO_ENABLED=0 go build -o /out/ozon-workbench ./cmd/server

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/ozon-workbench /usr/local/bin/ozon-workbench
EXPOSE 18081
ENTRYPOINT ["/usr/local/bin/ozon-workbench"]
