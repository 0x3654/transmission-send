# syntax=docker/dockerfile:1

# --- стадия тестов: парсер прогоняется на живом снимке топа NNMClub,
#     сломанный парсер валит сборку образа
FROM python:3.12-alpine AS test
WORKDIR /app
COPY server.py ./
COPY tests/ ./tests/
RUN python tests/test_server.py

# --- рантайм: только server.py, ничего от тестовой стадии
FROM python:3.12-alpine
ARG GIT_REV=unknown
LABEL org.opencontainers.image.title="tracker-top" \
      org.opencontainers.image.description="NNMClub top -> JSON для плагина Lampa" \
      org.opencontainers.image.source="https://github.com/0x3654/transmission-send" \
      org.opencontainers.image.revision="${GIT_REV}"
RUN apk add --no-cache tini
WORKDIR /app
COPY --from=test /app/server.py ./
ENV PORT=8355 \
    PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1
USER 65534:65534
EXPOSE 8355
HEALTHCHECK --interval=60s --timeout=5s --start-period=10s --retries=3 \
    CMD python -c "import os,urllib.request; urllib.request.urlopen('http://127.0.0.1:%s/healthz' % os.environ.get('PORT','8355'), timeout=3)"
ENTRYPOINT ["/sbin/tini", "--"]
CMD ["python", "/app/server.py"]
