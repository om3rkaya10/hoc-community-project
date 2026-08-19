# syntax=docker/dockerfile:1

FROM golang:1.24-bookworm AS build
WORKDIR /src
COPY server/hoc-server/go.mod ./
COPY server/hoc-server/ ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/hoc-server ./cmd/hoc-server

FROM debian:bookworm-slim
RUN groupadd --system hocserver && useradd --system --gid hocserver --home-dir /var/lib/hoc-server --create-home hocserver
WORKDIR /var/lib/hoc-server
COPY --from=build /out/hoc-server /usr/local/bin/hoc-server
COPY LICENSE NOTICE /usr/share/doc/hoc-community-server/
RUN chown root:root /usr/local/bin/hoc-server && chmod 0755 /usr/local/bin/hoc-server \
    && chown -R hocserver:hocserver /var/lib/hoc-server \
    && chmod 0755 /var/lib/hoc-server
USER hocserver
EXPOSE 80 443 8080 8443 9999 20001
ENTRYPOINT ["/usr/local/bin/hoc-server"]
