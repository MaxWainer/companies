# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM golang:1.27.2-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=local

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
ARG TARGETOS TARGETARCH
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" \
      -o /out/companies ./cmd/companies

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/companies /usr/local/bin/companies
EXPOSE 8080
USER nonroot:nonroot
HEALTHCHECK --interval=10s --timeout=5s --start-period=20s --retries=3 \
  CMD ["/usr/local/bin/companies", "healthcheck", "ready"]
ENTRYPOINT ["/usr/local/bin/companies"]
CMD ["serve"]
