# Final stage
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates libsqlite3-0 && \
    rm -rf /var/lib/apt/lists/*

COPY gateway-proxy-cgo /gateway-proxy
COPY config.yaml.example /app/config.yaml

EXPOSE 8080

WORKDIR /
CMD ["/gateway-proxy"]