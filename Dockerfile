# syntax=docker/dockerfile:1

# ---- build -----------------------------------------------------------------
FROM golang:1.23-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
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
