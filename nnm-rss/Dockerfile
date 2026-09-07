# syntax=docker/dockerfile:1
ARG GO=1.27-alpine

# --- зависимости: качаются отдельно от кода, слой живёт в кэше между сборками
FROM golang:${GO} AS deps
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

# --- тесты: парсеры гоняются на живых снимках страниц; сломанный парсер валит сборку
FROM deps AS test
COPY *.go ui.html ./
COPY tests/ ./tests/
RUN --mount=type=cache,target=/go/pkg/mod go test ./...

# --- сборка: статические бинарники (CGO выкл → scratch)
FROM deps AS build
ARG GIT_REV=unknown
COPY *.go ui.html ./
COPY cmd/ ./cmd/
RUN mkdir -p /data
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.rev=${GIT_REV}" -o /out/nnm-rss . && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/healthcheck ./cmd/healthcheck

# --- рантайм: scratch — два бинарника, UI и сертификаты; /data — под состояние (том)
FROM scratch
ARG GIT_REV=unknown
LABEL org.opencontainers.image.title="nnm-rss" \
      org.opencontainers.image.description="Личные RSS-ленты NNM-Club (мультиюзерный сервис в духе lostfilmfeed)" \
      org.opencontainers.image.source="https://github.com/0x3654/transmission-send" \
      org.opencontainers.image.revision="${GIT_REV}"
COPY --from=build /out/nnm-rss /nnm-rss
COPY --from=build /out/healthcheck /healthcheck
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build --chown=65534:65534 /data /data
USER 65534:65534
VOLUME /data
EXPOSE 8356
ENV PORT=8356 DATA_DIR=/data
HEALTHCHECK --interval=60s --timeout=5s --start-period=10s --retries=3 CMD ["/healthcheck"]
ENTRYPOINT ["/nnm-rss"]
