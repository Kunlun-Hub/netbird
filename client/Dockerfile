# build & run locally with:
#   cd "$(git rev-parse --show-toplevel)"
#   CGO_ENABLED=0 go build -o cloink ./client
#   sudo podman build -t localhost/cloink:latest -f client/Dockerfile --ignorefile .dockerignore-client .
#   sudo podman run --rm -it --cap-add={BPF,NET_ADMIN,NET_RAW} localhost/cloink:latest

FROM alpine:3.23.3
# iproute2: busybox doesn't display ip rules properly
RUN apk add --no-cache \
    bash \
    ca-certificates \
    ip6tables \
    iproute2 \
    iptables

ENV \
    NETBIRD_BIN="/usr/local/bin/cloink" \
    CL_LOG_FILE="console,/var/log/cloink/client.log" \
    CL_DAEMON_ADDR="unix:///var/run/cloink.sock" \
    CL_ENTRYPOINT_SERVICE_TIMEOUT="30" \
    NB_LOG_FILE="console,/var/log/cloink/client.log" \
    NB_DAEMON_ADDR="unix:///var/run/cloink.sock" \
    NB_ENTRYPOINT_SERVICE_TIMEOUT="30"

ENTRYPOINT [ "/usr/local/bin/cloink-entrypoint.sh" ]

ARG NETBIRD_BINARY=cloink
COPY client/cloink-entrypoint.sh /usr/local/bin/cloink-entrypoint.sh
COPY "${NETBIRD_BINARY}"  /usr/local/bin/cloink
