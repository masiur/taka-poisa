# syntax=docker/dockerfile:1

# 1. Build the web UI into internal/webui/dist.
FROM node:22-alpine AS web
WORKDIR /src/web
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN mkdir -p ../internal/webui/dist && pnpm build

# 2. Build the Go binary with the UI embedded.
#    Self-hosted image: no tags. Hosted app: --build-arg GO_TAGS=hosted (adds Google sign-in).
FROM golang:1.27-alpine AS build
ARG GO_TAGS=""
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/webui/dist ./internal/webui/dist
RUN CGO_ENABLED=0 go build -tags "$GO_TAGS" -trimpath -ldflags="-s -w" -o /out/takapoisa ./cmd/takapoisa

# 3. Minimal runtime image, non-root.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/takapoisa /takapoisa
EXPOSE 8080
ENTRYPOINT ["/takapoisa"]
