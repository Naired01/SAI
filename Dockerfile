# syntax=docker/dockerfile:1.7

# -----------------------------------------------------------------------------
# Stage 1: build del panel web
# -----------------------------------------------------------------------------
FROM node:22-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json* ./
RUN npm ci --no-audit --no-fund || npm install --no-audit --no-fund
COPY web/ ./
RUN npm run build

# -----------------------------------------------------------------------------
# Stage 2: build del server (Go) + cross-build del agente
# -----------------------------------------------------------------------------
FROM golang:1.25-alpine AS go
RUN apk add --no-cache git make
WORKDIR /src

# Copiar go.mod/go.sum primero para cache de capas
COPY go.mod go.sum ./
RUN go mod download

# Copiar el resto
COPY . .

# Variables de build inyectadas en el binario
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown
ENV CGO_ENABLED=0 GOOS=linux

# Build del server
RUN go build -trimpath \
    -ldflags="-s -w -X github.com/Naired01/SAI/internal/version.Version=${VERSION} \
              -X github.com/Naired01/SAI/internal/version.Commit=${COMMIT} \
              -X github.com/Naired01/SAI/internal/version.BuildTime=${BUILD_TIME}" \
    -o /out/sai-server ./cmd/server

# Cross-build del agente para todas las plataformas soportadas
RUN mkdir -p /out/dist && \
    for target in "windows amd64 .exe" "linux amd64" "linux arm64" "darwin amd64" "darwin arm64"; do \
      set -- $target; \
      GOOS=$1 GOARCH=$2 SUFFIX=$3; \
      go build -trimpath \
        -ldflags="-s -w -X github.com/Naired01/SAI/internal/version.Version=${VERSION} \
                  -X github.com/Naired01/SAI/internal/version.Commit=${COMMIT}" \
        -o /out/dist/sai-agent-${1}-${2}${3} ./cmd/agent; \
    done

# -----------------------------------------------------------------------------
# Stage 3: imagen final (distroless, multi-arch)
# -----------------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=go /out/sai-server /app/sai-server
COPY --from=web /web/dist /app/web
COPY --from=go /out/dist /app/dist

USER nonroot:nonroot
EXPOSE 8080
ENV SAI_ENV=production \
    SAI_BIND=:8080 \
    SAI_BUNDLE_DIR=/app/dist \
    SAI_WEB_DIST=/app/web

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/app/sai-server", "--healthcheck"]

ENTRYPOINT ["/app/sai-server"]