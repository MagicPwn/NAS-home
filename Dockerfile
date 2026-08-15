FROM node:22-bookworm-slim@sha256:d649c27dae7ba0137b3cef5dd75baa422c08dc3d9e3fc0c23dfb172dc3cc6436 AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json frontend/tsconfig.json frontend/vite.config.ts ./
COPY frontend/index.html ./
COPY frontend/src ./src
RUN npm ci --no-audit --no-fund && npm run build

FROM golang:1.23-alpine@sha256:383395b794dffa5b53012a212365d40c8e37109a626ca30d6151c8348d380b5f AS backend
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/nas-home ./cmd/nas-home

FROM golang:1.23-alpine@sha256:383395b794dffa5b53012a212365d40c8e37109a626ca30d6151c8348d380b5f AS proxy
WORKDIR /src/proxy
COPY deploy/socket-proxy/main.go ./main.go
RUN CGO_ENABLED=0 GO111MODULE=off go build -trimpath -ldflags='-s -w' -o /out/socket-proxy ./main.go

FROM alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d AS runtime
RUN apk add --no-cache ca-certificates && addgroup -S -g 10001 nas-home && adduser -S -D -u 10001 -G nas-home nas-home && mkdir -p /data && chown -R nas-home:nas-home /data
COPY --from=backend /out/nas-home /usr/local/bin/nas-home
COPY --from=frontend /src/frontend/dist /app/frontend
USER nas-home
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/nas-home"]

FROM alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d AS socket-proxy
RUN apk add --no-cache ca-certificates
COPY --from=proxy /out/socket-proxy /usr/local/bin/socket-proxy
EXPOSE 2375
ENTRYPOINT ["/usr/local/bin/socket-proxy"]
