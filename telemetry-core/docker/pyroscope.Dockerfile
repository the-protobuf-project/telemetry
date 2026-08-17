FROM ubuntu:20.04 AS curl

RUN apt update && \
    apt install -y wget && \
    apt clean

RUN if [ "$(uname -m)" = "x86_64" ]; then \
    wget https://github.com/moparisthebest/static-curl/releases/download/v8.7.1/curl-amd64 -O /usr/bin/curl; \
    elif [ "$(uname -m)" = "aarch64" ]; then \
    wget https://github.com/moparisthebest/static-curl/releases/download/v8.7.1/curl-aarch64 -O /usr/bin/curl; \
    fi && \
    chmod +x /usr/bin/curl

FROM grafana/pyroscope:latest
COPY --from=curl /usr/bin/curl /usr/bin/curl
