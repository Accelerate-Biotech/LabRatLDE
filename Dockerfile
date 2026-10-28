# syntax=docker/dockerfile:1

# ---- ui --------------------------------------------------------------------
FROM node:22-alpine AS ui
WORKDIR /web

COPY web/package.json web/package-lock.json ./
RUN npm ci

COPY web/ ./
RUN npm run build

# ---- build -----------------------------------------------------------------
FROM golang:1.25-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
COPY --from=ui /web/dist ./web/dist
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags="-s -w -X main.version=${VERSION}" \
      -o /out/labrat ./cmd/server

# ---- runtime ---------------------------------------------------------------
# Static binary + CA certs + nonroot user. No shell, no package manager.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/labrat /labrat
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/labrat"]
