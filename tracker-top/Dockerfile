# syntax=docker/dockerfile:1
ARG GO=1.27-alpine

# --- зависимости: качаются отдельно от кода, слой живёт в кэше между сборками
FROM golang:${GO} AS deps
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

# --- тесты: парсеры гоняются на живых снимках страниц; сломанный парсер валит сборку
FROM deps AS test
COPY *.go ./
COPY tests/ ./tests/
RUN --mount=type=cache,target=/go/pkg/mod go test ./...

# --- сборка: статические бинарники (CGO выкл → scratch)
FROM deps AS build
ARG GIT_REV=unknown
COPY *.go ./
COPY cmd/ ./cmd/
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.rev=${GIT_REV}" -o /out/tracker-top . && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/healthcheck ./cmd/healthcheck

# --- рантайм: scratch — только два бинарника и сертификаты для https-запросов к трекерам
FROM scratch
ARG GIT_REV=unknown
LABEL org.opencontainers.image.title="tracker-top" \
      org.opencontainers.image.description="Топ раздач трекеров (NNMClub, RUTOR) -> JSON для плагина Lampa" \
      org.opencontainers.image.source="https://github.com/0x3654/transmission-send" \
      org.opencontainers.image.revision="${GIT_REV}"
COPY --from=build /out/tracker-top /tracker-top
COPY --from=build /out/healthcheck /healthcheck
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
USER 65534:65534
EXPOSE 8355
ENV PORT=8355
HEALTHCHECK --interval=60s --timeout=5s --start-period=10s --retries=3 CMD ["/healthcheck"]
ENTRYPOINT ["/tracker-top"]
