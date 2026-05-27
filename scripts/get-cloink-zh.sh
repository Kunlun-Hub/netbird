#!/bin/bash

set -e

# Cloink 内置身份提供方安装脚本
# 使用内置 Dex 身份提供方部署 Cloink
# 不需要单独的 Dex 容器；身份提供方集成在 management server 中

# Sed pattern to strip base64 padding characters
SED_STRIP_PADDING='s/=//g'

# Constants for repeated string literals
readonly MSG_STARTING_SERVICES="\n正在启动 Cloink 服务\n"
readonly MSG_DONE="\n完成！\n"
readonly MSG_NEXT_STEPS="下一步："
readonly MSG_SEPARATOR="=========================================="

############################################
# Utility Functions
############################################

check_docker_compose() {
  if command -v docker-compose &> /dev/null
  then
      echo "docker-compose"
      return
  fi
  if docker compose --help &> /dev/null
  then
      echo "docker compose"
      return
  fi

    echo "docker-compose 未安装或不在 PATH 中，请参考 Docker 官方文档安装：https://docs.docker.com/engine/install/" > /dev/stderr
  exit 1
}

check_jq() {
  if ! command -v jq &> /dev/null
  then
    echo "jq 未安装或不在 PATH 中，请使用系统包管理器安装，例如：sudo apt install jq" > /dev/stderr
    exit 1
  fi
  return 0
}

get_main_ip_address() {
  if [[ "$OSTYPE" == "darwin"* ]]; then
    interface=$(route -n get default | grep 'interface:' | awk '{print $2}')
    ip_address=$(ifconfig "$interface" | grep 'inet ' | awk '{print $2}')
  else
    interface=$(ip route | grep default | awk '{print $5}' | head -n 1)
    ip_address=$(ip addr show "$interface" | grep 'inet ' | awk '{print $2}' | cut -d'/' -f1)
  fi

  echo "$ip_address"
  return 0
}

check_nb_domain() {
  DOMAIN=$1
  if [[ "$DOMAIN-x" == "-x" ]]; then
    echo "NETBIRD_DOMAIN 不能为空。" > /dev/stderr
    return 1
  fi

  if [[ "$DOMAIN" == "netbird.example.com" ]]; then
    echo "NETBIRD_DOMAIN 不能使用 netbird.example.com。" > /dev/stderr
    return 1
  fi
  return 0
}

read_nb_domain() {
  READ_NETBIRD_DOMAIN=""
  echo -n "请输入 Cloink 使用的域名（例如 cloink.example.com）： " > /dev/stderr
  read -r READ_NETBIRD_DOMAIN < /dev/tty
  if ! check_nb_domain "$READ_NETBIRD_DOMAIN"; then
    read_nb_domain
  fi
  echo "$READ_NETBIRD_DOMAIN"
  return 0
}

read_reverse_proxy_type() {
  echo "" > /dev/stderr
  echo "请选择要使用的反向代理：" > /dev/stderr
  echo "  [0] Traefik（推荐，Docker Compose 内置，自动 TLS）" > /dev/stderr
  echo "  [1] 已有 Traefik（为外部 Traefik 生成 labels）" > /dev/stderr
  echo "  [2] Nginx（生成配置模板）" > /dev/stderr
  echo "  [3] Nginx Proxy Manager（生成配置和说明）" > /dev/stderr
  echo "  [4] 外部 Caddy（生成 Caddyfile 片段）" > /dev/stderr
  echo "  [5] 其他/手动配置（输出配置说明）" > /dev/stderr
  echo "" > /dev/stderr
  echo -n "请输入选项 [0-5]（默认：0）： " > /dev/stderr
  read -r CHOICE < /dev/tty

  if [[ -z "$CHOICE" ]]; then
    CHOICE="0"
  fi

  if [[ ! "$CHOICE" =~ ^[0-5]$ ]]; then
    echo "选项无效，请输入 0 到 5 之间的数字。" > /dev/stderr
    read_reverse_proxy_type
    return
  fi

  echo "$CHOICE"
  return 0
}

read_traefik_network() {
  echo "" > /dev/stderr
  echo "如果你已有 Traefik 实例，请输入它所在的外部 Docker 网络名。" > /dev/stderr
  echo -n "外部网络名（留空则创建 'netbird' 网络）： " > /dev/stderr
  read -r NETWORK < /dev/tty
  echo "$NETWORK"
  return 0
}

read_traefik_entrypoint() {
  echo "" > /dev/stderr
  echo "请输入 Traefik HTTPS entrypoint 名称。" > /dev/stderr
  echo -n "HTTPS entrypoint 名称（默认：websecure）： " > /dev/stderr
  read -r ENTRYPOINT < /dev/tty
  if [[ -z "$ENTRYPOINT" ]]; then
    ENTRYPOINT="websecure"
  fi
  echo "$ENTRYPOINT"
  return 0
}

read_traefik_certresolver() {
  echo "" > /dev/stderr
  echo "请输入 Traefik 证书解析器名称（用于自动 TLS）。" > /dev/stderr
  echo "如果你在其他地方终止 TLS，或使用通配符证书，可以留空。" > /dev/stderr
  echo -n "证书解析器名称（例如 letsencrypt）： " > /dev/stderr
  read -r RESOLVER < /dev/tty
  echo "$RESOLVER"
  return 0
}

read_port_binding_preference() {
  echo "" > /dev/stderr
  echo "容器端口是否只绑定到 localhost（127.0.0.1）？" > /dev/stderr
  echo "如果反向代理和 Cloink 在同一台主机，建议选择 yes，更安全。" > /dev/stderr
  echo -n "是否只绑定 localhost？[Y/n]： " > /dev/stderr
  read -r CHOICE < /dev/tty

  if [[ "$CHOICE" =~ ^[Nn]$ ]]; then
    echo "false"
  else
    echo "true"
  fi
  return 0
}

read_proxy_docker_network() {
  local proxy_name="$1"
  echo "" > /dev/stderr
  echo "${proxy_name} 是否运行在 Docker 中？" > /dev/stderr
  echo "如果是，请输入 ${proxy_name} 所在的 Docker 网络名，Cloink 会加入该网络。" > /dev/stderr
  echo -n "Docker 网络名（不在 Docker 中则留空）： " > /dev/stderr
  read -r NETWORK < /dev/tty
  echo "$NETWORK"
  return 0
}

read_enable_proxy() {
  echo "" > /dev/stderr
  echo "Do you want to enable the Cloink Proxy service?" > /dev/stderr
  echo "The proxy allows you to selectively expose internal Cloink network resources" > /dev/stderr
  echo "to the internet. You control which resources are exposed through the dashboard." > /dev/stderr
  echo -n "Enable proxy? [y/N]: " > /dev/stderr
  read -r CHOICE < /dev/tty

  if [[ "$CHOICE" =~ ^[Yy]$ ]]; then
    echo "true"
  else
    echo "false"
  fi
  return 0
}

read_enable_crowdsec() {
  echo "" > /dev/stderr
  echo "Do you want to enable CrowdSec IP reputation blocking?" > /dev/stderr
  echo "CrowdSec checks client IPs against a community threat intelligence database" > /dev/stderr
  echo "and blocks known malicious sources before they reach your services." > /dev/stderr
  echo "A local CrowdSec LAPI container will be added to your deployment." > /dev/stderr
  echo -n "Enable CrowdSec? [y/N]: " > /dev/stderr
  read -r CHOICE < /dev/tty

  if [[ "$CHOICE" =~ ^[Yy]$ ]]; then
    echo "true"
  else
    echo "false"
  fi
  return 0
}

read_traefik_acme_email() {
  echo "" > /dev/stderr
  echo "请输入用于接收 Let's Encrypt 证书通知的邮箱。" > /dev/stderr
  echo -n "邮箱地址： " > /dev/stderr
  read -r EMAIL < /dev/tty
  if [[ -z "$EMAIL" ]]; then
    echo "Let's Encrypt 需要邮箱地址。" > /dev/stderr
    read_traefik_acme_email
    return
  fi
  echo "$EMAIL"
  return 0
}

get_bind_address() {
  if [[ "$BIND_LOCALHOST_ONLY" == "true" ]]; then
    echo "127.0.0.1"
  else
    echo "0.0.0.0"
  fi
  return 0
}

get_upstream_host() {
  # Always return 127.0.0.1 for health checks and upstream targets
  # Cannot use 0.0.0.0 as a connection target
  echo "127.0.0.1"
  return 0
}

wait_management_proxy() {
  local proxy_container="${1:-traefik}"
  local use_docker_logs=false
  set +e

  if [[ "$proxy_container" == "detect-traefik" ]]; then
    proxy_container=$(docker ps --format "{{.ID}}\t{{.Image}}\t{{.Ports}}" \
    | awk -F'\t' '$2 ~ /traefik/ && $3 ~ /:(80|443)->/ {print $1; exit}')

    if [[ -z "$proxy_container" ]]; then
      echo "Warning: could not auto-detect Traefik container, log output will be skipped on timeout." > /dev/stderr
    else
      use_docker_logs=true
    fi
  fi

  echo -n "正在等待 Cloink 服务就绪"
  counter=1
  while true; do
    # Check the embedded IdP endpoint through the reverse proxy
    if curl -sk -f -o /dev/null "$NETBIRD_HTTP_PROTOCOL://$NETBIRD_DOMAIN/oauth2/.well-known/openid-configuration" 2>/dev/null; then
      break
    fi
    if [[ $counter -eq 60 ]]; then
      echo ""
      echo "等待时间较长，正在检查日志..."
      if [[ -n "$proxy_container" ]]; then
        if [[ "$use_docker_logs" == "true" ]]; then
          docker logs --tail=20 "$proxy_container"
        else
          $DOCKER_COMPOSE_COMMAND logs --tail=20 "$proxy_container"
        fi
      fi
      $DOCKER_COMPOSE_COMMAND logs --tail=20 netbird-server
    fi
    echo -n " ."
    sleep 2
    counter=$((counter + 1))
  done
  echo " done"
  set -e
  return 0
}

wait_management_direct() {
  set +e
  local upstream_host=$(get_upstream_host)
  echo -n "正在等待 Cloink 服务就绪"
  counter=1
  while true; do
    # Check the embedded IdP endpoint directly (no reverse proxy)
    if curl -sk -f -o /dev/null "http://${upstream_host}:${MANAGEMENT_HOST_PORT}/oauth2/.well-known/openid-configuration" 2>/dev/null; then
      break
    fi
    if [[ $counter -eq 60 ]]; then
      echo ""
      echo "等待时间较长，正在检查日志..."
      $DOCKER_COMPOSE_COMMAND logs --tail=20 netbird-server
    fi
    echo -n " ."
    sleep 2
    counter=$((counter + 1))
  done
  echo " done"
  set -e
  return 0
}

############################################
# Initialization and Configuration
############################################

initialize_default_values() {
  NETBIRD_PORT=80
  NETBIRD_HTTP_PROTOCOL="http"
  NETBIRD_RELAY_PROTO="rel"
  NETBIRD_RELAY_AUTH_SECRET=$(openssl rand -base64 32 | sed "$SED_STRIP_PADDING")
  # Note: DataStoreEncryptionKey must keep base64 padding (=) for Go's base64.StdEncoding
  DATASTORE_ENCRYPTION_KEY=$(openssl rand -base64 32)
  NETBIRD_STUN_PORT=3478

  # Docker images
  DASHBOARD_IMAGE="ohoimager/cloink-dashboard:flow-dev"
  # Combined server replaces separate signal, relay, and management containers
  NETBIRD_SERVER_IMAGE="ohoimager/cloink-server:flow-dev"
  NETBIRD_PROXY_IMAGE="netbirdio/reverse-proxy:latest"

  # Reverse proxy configuration
  REVERSE_PROXY_TYPE="0"
  TRAEFIK_EXTERNAL_NETWORK=""
  TRAEFIK_ENTRYPOINT="websecure"
  TRAEFIK_CERTRESOLVER=""
  TRAEFIK_ACME_EMAIL=""
  DASHBOARD_HOST_PORT="8080"
  MANAGEMENT_HOST_PORT="8081"  # Combined server port (management + signal + relay)
  BIND_LOCALHOST_ONLY="true"
  EXTERNAL_PROXY_NETWORK=""

  # Traefik static IP within the internal bridge network
  TRAEFIK_IP="172.30.0.10"

  # Cloink Proxy configuration
  ENABLE_PROXY="false"
  PROXY_TOKEN=""

  # CrowdSec configuration
  ENABLE_CROWDSEC="false"
  CROWDSEC_BOUNCER_KEY=""
  return 0
}

configure_domain() {
  if ! check_nb_domain "$NETBIRD_DOMAIN"; then
    NETBIRD_DOMAIN=$(read_nb_domain)
  fi

  if [[ "$NETBIRD_DOMAIN" == "use-ip" ]]; then
    NETBIRD_DOMAIN=$(get_main_ip_address)
    BASE_DOMAIN=$NETBIRD_DOMAIN
  else
    NETBIRD_PORT=443
    NETBIRD_HTTP_PROTOCOL="https"
    NETBIRD_RELAY_PROTO="rels"
    BASE_DOMAIN=$(echo $NETBIRD_DOMAIN | sed -E 's/^[^.]+\.//')
  fi
  return 0
}

configure_reverse_proxy() {
  # Prompt for reverse proxy type
  REVERSE_PROXY_TYPE=$(read_reverse_proxy_type)

  # Handle built-in Traefik prompts (option 0)
  if [[ "$REVERSE_PROXY_TYPE" == "0" ]]; then
    TRAEFIK_ACME_EMAIL=$(read_traefik_acme_email)
    ENABLE_PROXY=$(read_enable_proxy)
    if [[ "$ENABLE_PROXY" == "true" ]]; then
      ENABLE_CROWDSEC=$(read_enable_crowdsec)
    fi
  fi

  # Handle external Traefik-specific prompts (option 1)
  if [[ "$REVERSE_PROXY_TYPE" == "1" ]]; then
    TRAEFIK_EXTERNAL_NETWORK=$(read_traefik_network)
    TRAEFIK_ENTRYPOINT=$(read_traefik_entrypoint)
    TRAEFIK_CERTRESOLVER=$(read_traefik_certresolver)
  fi

  # Handle port binding for external proxy options (2-5)
  if [[ "$REVERSE_PROXY_TYPE" -ge 2 ]]; then
    BIND_LOCALHOST_ONLY=$(read_port_binding_preference)
  fi

  # Handle Docker network prompts for external proxies (options 2-4)
  case "$REVERSE_PROXY_TYPE" in
    2) EXTERNAL_PROXY_NETWORK=$(read_proxy_docker_network "Nginx") ;;
    3) EXTERNAL_PROXY_NETWORK=$(read_proxy_docker_network "Nginx Proxy Manager") ;;
    4) EXTERNAL_PROXY_NETWORK=$(read_proxy_docker_network "Caddy") ;;
    *) ;; # No network prompt for other options
  esac
  return 0
}

check_existing_installation() {
  if [[ -f config.yaml ]]; then
    echo "Generated files already exist, if you want to reinitialize the environment, please remove them first."
    echo "You can use the following commands:"
    echo "  $DOCKER_COMPOSE_COMMAND down --volumes # to remove all containers and volumes"
    echo "  rm -f docker-compose.yml dashboard.env config.yaml proxy.env traefik-dynamic.yaml cloink-nginx.conf caddyfile-cloink.txt npm-advanced-config.txt && rm -rf crowdsec/"
    echo "Be aware that this will remove all data from the database, and you will have to reconfigure the dashboard."
    exit 1
  fi
  return 0
}

generate_configuration_files() {
  echo Rendering initial files...

  # Render docker-compose and proxy config based on selection
  case "$REVERSE_PROXY_TYPE" in
    0)
      render_docker_compose_traefik_builtin > docker-compose.yml
      if [[ "$ENABLE_PROXY" == "true" ]]; then
        # Create placeholder proxy.env so docker-compose can validate
        # This will be overwritten with the actual token after netbird-server starts
        echo "# Placeholder - will be updated with token after netbird-server starts" > proxy.env
        echo "NB_PROXY_TOKEN=placeholder" >> proxy.env
        # TCP ServersTransport for PROXY protocol v2 to the proxy backend
        render_traefik_dynamic > traefik-dynamic.yaml
        if [[ "$ENABLE_CROWDSEC" == "true" ]]; then
          mkdir -p crowdsec
        fi
      fi
      ;;
    1)
      render_docker_compose_traefik > docker-compose.yml
      ;;
    2)
      render_docker_compose_exposed_ports > docker-compose.yml
      render_nginx_conf > cloink-nginx.conf
      ;;
    3)
      render_docker_compose_exposed_ports > docker-compose.yml
      render_npm_advanced_config > npm-advanced-config.txt
      ;;
    4)
      render_docker_compose_exposed_ports > docker-compose.yml
      render_external_caddyfile > caddyfile-cloink.txt
      ;;
    5)
      render_docker_compose_exposed_ports > docker-compose.yml
      ;;
    *)
      echo "无效的反向代理类型：$REVERSE_PROXY_TYPE" > /dev/stderr
      exit 1
      ;;
  esac

  # Common files for all configurations
  render_dashboard_env > dashboard.env
  render_combined_yaml > config.yaml
  return 0
}

start_services_and_show_instructions() {
  # For built-in Traefik, start containers immediately
  # For NPM, start containers first (NPM needs services running to create proxy)
  # For other external proxies, show instructions first and wait for user confirmation
  if [[ "$REVERSE_PROXY_TYPE" == "0" ]]; then
    # Built-in Traefik - two-phase startup if proxy is enabled
    echo -e "$MSG_STARTING_SERVICES"

    if [[ "$ENABLE_PROXY" == "true" ]]; then
      # Phase 1: Start core services (without proxy)
      local core_services="traefik dashboard netbird-server"
      if [[ "$ENABLE_CROWDSEC" == "true" ]]; then
        core_services="$core_services crowdsec"
      fi
      echo "Starting core services..."
      $DOCKER_COMPOSE_COMMAND up -d $core_services

      sleep 3
      wait_management_proxy traefik

      # Phase 2: Create proxy token and start proxy
      echo ""
      echo "Creating proxy access token..."
      # Use docker exec with bash to run the token command directly
      PROXY_TOKEN=$($DOCKER_COMPOSE_COMMAND exec -T netbird-server \
        /go/bin/netbird-server token create --name "default-proxy" --config /etc/netbird/config.yaml 2>/dev/null | grep "^Token:" | awk '{print $2}')

      if [[ -z "$PROXY_TOKEN" ]]; then
        echo "ERROR: Failed to create proxy token. Check netbird-server logs." > /dev/stderr
        $DOCKER_COMPOSE_COMMAND logs --tail=20 netbird-server
        exit 1
      fi

      echo "Proxy token created successfully."

      if [[ "$ENABLE_CROWDSEC" == "true" ]]; then
        echo "Registering CrowdSec bouncer..."
        local cs_retries=0
        while ! $DOCKER_COMPOSE_COMMAND exec -T crowdsec cscli lapi status >/dev/null 2>&1; do
          cs_retries=$((cs_retries + 1))
          if [[ $cs_retries -ge 30 ]]; then
            echo "WARNING: CrowdSec did not become ready. Skipping CrowdSec setup." > /dev/stderr
            echo "You can register a bouncer manually later with:" > /dev/stderr
            echo "  docker exec netbird-crowdsec cscli bouncers add netbird-proxy -o raw" > /dev/stderr
            ENABLE_CROWDSEC="false"
            break
          fi
          sleep 2
        done

        if [[ "$ENABLE_CROWDSEC" == "true" ]]; then
          CROWDSEC_BOUNCER_KEY=$($DOCKER_COMPOSE_COMMAND exec -T crowdsec \
            cscli bouncers add netbird-proxy -o raw 2>/dev/null)
          if [[ -z "$CROWDSEC_BOUNCER_KEY" ]]; then
            echo "WARNING: Failed to create CrowdSec bouncer key. Skipping CrowdSec setup." > /dev/stderr
            ENABLE_CROWDSEC="false"
          else
            echo "CrowdSec bouncer registered."
          fi
        fi
      fi

      render_proxy_env > proxy.env

      # Start proxy service
      echo "Starting proxy service..."
      $DOCKER_COMPOSE_COMMAND up -d proxy
    else
      # No proxy - start all services at once
      $DOCKER_COMPOSE_COMMAND up -d

      sleep 3
      wait_management_proxy traefik
    fi

    echo -e "$MSG_DONE"
    print_post_setup_instructions
  elif [[ "$REVERSE_PROXY_TYPE" == "1" ]]; then
    # External Traefik - start containers, then show instructions
    # Traefik discovers services via Docker labels, so containers must be running
    echo -e "$MSG_STARTING_SERVICES"
    $DOCKER_COMPOSE_COMMAND up -d

    sleep 3
    wait_management_proxy detect-traefik

    echo -e "$MSG_DONE"
    print_post_setup_instructions
    echo ""
    echo "Cloink 容器已运行。Traefik 接入后，可通过以下地址访问控制台："
    echo "  $NETBIRD_HTTP_PROTOCOL://$NETBIRD_DOMAIN"
  elif [[ "$REVERSE_PROXY_TYPE" == "3" ]]; then
    # NPM - start containers first, then show instructions
    # NPM requires backend services to be running before creating proxy hosts
    echo -e "$MSG_STARTING_SERVICES"
    $DOCKER_COMPOSE_COMMAND up -d

    sleep 3
    wait_management_direct

    echo -e "$MSG_DONE"
    print_post_setup_instructions
    echo ""
    echo "Cloink 容器已运行。按上面的说明配置 NPM 后，可通过以下地址访问："
    echo "  $NETBIRD_HTTP_PROTOCOL://$NETBIRD_DOMAIN"
  else
    # External proxies (nginx, external Caddy, other) - need manual config first
    print_post_setup_instructions

    echo ""
    echo -n "反向代理配置完成后按 Enter 继续（或按 Ctrl+C 退出）... "
    read -r < /dev/tty

    echo -e "$MSG_STARTING_SERVICES"
    $DOCKER_COMPOSE_COMMAND up -d

    sleep 3
    wait_management_direct

    echo -e "$MSG_DONE"
    echo "Cloink 已运行，可通过以下地址访问控制台："
    echo "  $NETBIRD_HTTP_PROTOCOL://$NETBIRD_DOMAIN"
  fi
  return 0
}

init_environment() {
  initialize_default_values
  configure_domain
  configure_reverse_proxy

  check_jq
  DOCKER_COMPOSE_COMMAND=$(check_docker_compose)

  check_existing_installation
  generate_configuration_files
  start_services_and_show_instructions
  return 0
}

############################################
# Configuration File Renderers
############################################

render_docker_compose_traefik_builtin() {
  # Generate proxy service section and Traefik dynamic config if enabled
  local proxy_service=""
  local proxy_volumes=""
  local crowdsec_service=""
  local crowdsec_volumes=""
  local traefik_file_provider=""
  local traefik_dynamic_volume=""
  if [[ "$ENABLE_PROXY" == "true" ]]; then
    traefik_file_provider='      - "--providers.file.filename=/etc/traefik/dynamic.yaml"'
    traefik_dynamic_volume="      - ./traefik-dynamic.yaml:/etc/traefik/dynamic.yaml:ro"

    local proxy_depends="
      netbird-server:
        condition: service_started"
    if [[ "$ENABLE_CROWDSEC" == "true" ]]; then
      proxy_depends="
      netbird-server:
        condition: service_started
      crowdsec:
        condition: service_healthy"
    fi

    proxy_service="
  # Cloink Proxy - exposes internal resources to the internet
  proxy:
    image: $NETBIRD_PROXY_IMAGE
    container_name: netbird-proxy
    ports:
    - 51820:51820/udp
    restart: unless-stopped
    networks: [netbird]
    depends_on:${proxy_depends}
    env_file:
      - ./proxy.env
    volumes:
      - netbird_proxy_certs:/certs
    labels:
      # TCP passthrough for any unmatched domain (proxy handles its own TLS)
      - traefik.enable=true
      - traefik.tcp.routers.proxy-passthrough.entrypoints=websecure
      - traefik.tcp.routers.proxy-passthrough.rule=HostSNI(\`*\`)
      - traefik.tcp.routers.proxy-passthrough.tls.passthrough=true
      - traefik.tcp.routers.proxy-passthrough.service=proxy-tls
      - traefik.tcp.routers.proxy-passthrough.priority=1
      - traefik.tcp.services.proxy-tls.loadbalancer.server.port=8443
      - traefik.tcp.services.proxy-tls.loadbalancer.serverstransport=pp-v2@file
    logging:
      driver: \"json-file\"
      options:
        max-size: \"500m\"
        max-file: \"2\"
"
    proxy_volumes="
  netbird_proxy_certs:"

    if [[ "$ENABLE_CROWDSEC" == "true" ]]; then
      crowdsec_service="
  crowdsec:
    image: crowdsecurity/crowdsec:v1.7.7
    container_name: netbird-crowdsec
    restart: unless-stopped
    networks: [netbird]
    environment:
      COLLECTIONS: crowdsecurity/linux
    volumes:
      - ./crowdsec:/etc/crowdsec
      - crowdsec_db:/var/lib/crowdsec/data
    healthcheck:
      test: ["CMD", "cscli", "lapi", "status"]
      interval: 10s
      timeout: 5s
      retries: 15
    labels:
      - traefik.enable=false
    logging:
      driver: \"json-file\"
      options:
        max-size: \"500m\"
        max-file: \"2\"
"
      crowdsec_volumes="
  crowdsec_db:"
    fi
  fi

  cat <<EOF
services:
  # Traefik reverse proxy (automatic TLS via Let's Encrypt)
  traefik:
    image: traefik:v3.6
    container_name: netbird-traefik
    restart: unless-stopped
    networks:
      netbird:
        ipv4_address: $TRAEFIK_IP
    command:
      # Logging
      - "--log.level=INFO"
      - "--accesslog=true"
      # Docker provider
      - "--providers.docker=true"
      - "--providers.docker.exposedbydefault=false"
      - "--providers.docker.network=netbird"
      # Entrypoints
      - "--entrypoints.web.address=:80"
      - "--entrypoints.websecure.address=:443"
      - "--entrypoints.websecure.allowACMEByPass=true"
      # Disable timeouts for long-lived gRPC streams
      - "--entrypoints.websecure.transport.respondingTimeouts.readTimeout=0"
      - "--entrypoints.websecure.transport.respondingTimeouts.writeTimeout=0"
      - "--entrypoints.websecure.transport.respondingTimeouts.idleTimeout=0"
      # HTTP to HTTPS redirect
      - "--entrypoints.web.http.redirections.entrypoint.to=websecure"
      - "--entrypoints.web.http.redirections.entrypoint.scheme=https"
      # Let's Encrypt ACME
      - "--certificatesresolvers.letsencrypt.acme.email=$TRAEFIK_ACME_EMAIL"
      - "--certificatesresolvers.letsencrypt.acme.storage=/letsencrypt/acme.json"
      - "--certificatesresolvers.letsencrypt.acme.tlschallenge=true"
      # gRPC transport settings
      - "--serverstransport.forwardingtimeouts.responseheadertimeout=0s"
      - "--serverstransport.forwardingtimeouts.idleconntimeout=0s"
$traefik_file_provider
    ports:
      - '443:443'
      - '80:80'
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - netbird_traefik_letsencrypt:/letsencrypt
$traefik_dynamic_volume
    logging:
      driver: "json-file"
      options:
        max-size: "500m"
        max-file: "2"

  # UI dashboard
  dashboard:
    image: $DASHBOARD_IMAGE
    container_name: netbird-dashboard
    restart: unless-stopped
    networks: [netbird]
    env_file:
      - ./dashboard.env
    labels:
      - traefik.enable=true
      - traefik.http.routers.netbird-dashboard.rule=Host(\`$NETBIRD_DOMAIN\`)
      - traefik.http.routers.netbird-dashboard.entrypoints=websecure
      - traefik.http.routers.netbird-dashboard.tls=true
      - traefik.http.routers.netbird-dashboard.tls.certresolver=letsencrypt
      - traefik.http.routers.netbird-dashboard.service=dashboard
      - traefik.http.routers.netbird-dashboard.priority=1
      - traefik.http.services.dashboard.loadbalancer.server.port=80
    logging:
      driver: "json-file"
      options:
        max-size: "500m"
        max-file: "2"

  # Combined server (Management + Signal + Relay + STUN)
  netbird-server:
    image: $NETBIRD_SERVER_IMAGE
    container_name: netbird-server
    restart: unless-stopped
    networks: [netbird]
    ports:
      - '$NETBIRD_STUN_PORT:$NETBIRD_STUN_PORT/udp'
    volumes:
      - ./cloink_data:/var/lib/netbird
      - ./config.yaml:/etc/netbird/config.yaml
    command: ["--config", "/etc/netbird/config.yaml"]
    labels:
      - traefik.enable=true
      # gRPC router (needs h2c backend for HTTP/2 cleartext)
      - traefik.http.routers.netbird-grpc.rule=Host(\`$NETBIRD_DOMAIN\`) && (PathPrefix(\`/signalexchange.SignalExchange/\`) || PathPrefix(\`/management.ManagementService/\`) || PathPrefix(\`/flow.FlowService/\`))
      - traefik.http.routers.netbird-grpc.entrypoints=websecure
      - traefik.http.routers.netbird-grpc.tls=true
      - traefik.http.routers.netbird-grpc.tls.certresolver=letsencrypt
      - traefik.http.routers.netbird-grpc.service=netbird-server-h2c
      - traefik.http.routers.netbird-grpc.priority=100
      # Backend router (relay, WebSocket, log APIs, API, OAuth2)
      - traefik.http.routers.netbird-backend.rule=Host(\`$NETBIRD_DOMAIN\`) && (Path(\`/relay\`) || PathPrefix(\`/relay/\`) || PathPrefix(\`/ws-proxy/\`) || PathPrefix(\`/api/events/audit\`) || PathPrefix(\`/api/events/proxy\`) || PathPrefix(\`/api/events/network-traffic\`) || PathPrefix(\`/api\`) || PathPrefix(\`/oauth2\`))
      - traefik.http.routers.netbird-backend.entrypoints=websecure
      - traefik.http.routers.netbird-backend.tls=true
      - traefik.http.routers.netbird-backend.tls.certresolver=letsencrypt
      - traefik.http.routers.netbird-backend.service=netbird-server
      - traefik.http.routers.netbird-backend.priority=100
      # Services
      - traefik.http.services.netbird-server.loadbalancer.server.port=80
      - traefik.http.services.netbird-server-h2c.loadbalancer.server.port=80
      - traefik.http.services.netbird-server-h2c.loadbalancer.server.scheme=h2c
    logging:
      driver: "json-file"
      options:
        max-size: "500m"
        max-file: "2"
${proxy_service}${crowdsec_service}
volumes:
  netbird_data:
  netbird_traefik_letsencrypt:${proxy_volumes}${crowdsec_volumes}

networks:
  netbird:
    driver: bridge
    ipam:
      config:
        - subnet: 172.30.0.0/24
          gateway: 172.30.0.1
EOF
  return 0
}

render_combined_yaml() {
  cat <<EOF
# Combined Cloink Server Configuration (Simplified)
# Generated by getting-started.sh

server:
  listenAddress: ":80"
  exposedAddress: "$NETBIRD_HTTP_PROTOCOL://$NETBIRD_DOMAIN:$NETBIRD_PORT"
  stunPorts:
    - $NETBIRD_STUN_PORT
  metricsPort: 9090
  healthcheckAddress: ":9000"
  logLevel: "info"
  logFile: "console"

  authSecret: "$NETBIRD_RELAY_AUTH_SECRET"
  dataDir: "/var/lib/netbird"

  auth:
    issuer: "$NETBIRD_HTTP_PROTOCOL://$NETBIRD_DOMAIN/oauth2"
    signKeyRefreshEnabled: true
    dashboardRedirectURIs:
      - "$NETBIRD_HTTP_PROTOCOL://$NETBIRD_DOMAIN/nb-auth"
      - "$NETBIRD_HTTP_PROTOCOL://$NETBIRD_DOMAIN/nb-silent-auth"
    cliRedirectURIs:
      - "http://localhost:53000/"

  reverseProxy:
    trustedHTTPProxies:
      - "$TRAEFIK_IP/32"

  store:
    engine: "sqlite"
    encryptionKey: "$DATASTORE_ENCRYPTION_KEY"
EOF
  return 0
}
render_dashboard_env() {
  cat <<EOF
# Endpoints
NETBIRD_MGMT_API_ENDPOINT=$NETBIRD_HTTP_PROTOCOL://$NETBIRD_DOMAIN
NETBIRD_MGMT_GRPC_API_ENDPOINT=$NETBIRD_HTTP_PROTOCOL://$NETBIRD_DOMAIN
# OIDC - using embedded IdP
AUTH_AUDIENCE=netbird-dashboard
AUTH_CLIENT_ID=netbird-dashboard
AUTH_CLIENT_SECRET=
AUTH_AUTHORITY=$NETBIRD_HTTP_PROTOCOL://$NETBIRD_DOMAIN/oauth2
USE_AUTH0=false
AUTH_SUPPORTED_SCOPES=openid profile email groups
AUTH_REDIRECT_URI=/nb-auth
AUTH_SILENT_REDIRECT_URI=/nb-silent-auth
# SSL
NGINX_SSL_PORT=443
# Letsencrypt
LETSENCRYPT_DOMAIN=none
EOF
  return 0
}

render_traefik_dynamic() {
  cat <<'EOF'
tcp:
  serversTransports:
    pp-v2:
      proxyProtocol:
        version: 2
EOF
  return 0
}

render_proxy_env() {
  cat <<EOF
# Cloink Proxy Configuration
NB_PROXY_DEBUG_LOGS=false
# Use internal Docker network to connect to management (avoids hairpin NAT issues)
NB_PROXY_MANAGEMENT_ADDRESS=http://netbird-server:80
# Allow insecure gRPC connection to management (required for internal Docker network)
NB_PROXY_ALLOW_INSECURE=true
# Public URL where this proxy is reachable (used for cluster registration)
NB_PROXY_DOMAIN=$NETBIRD_DOMAIN
NB_PROXY_ADDRESS=:8443
NB_PROXY_TOKEN=$PROXY_TOKEN
NB_PROXY_CERTIFICATE_DIRECTORY=/certs
NB_PROXY_ACME_CERTIFICATES=true
NB_PROXY_ACME_CHALLENGE_TYPE=tls-alpn-01
NB_PROXY_FORWARDED_PROTO=https
# Enable PROXY protocol to preserve client IPs through L4 proxies (Traefik TCP passthrough)
NB_PROXY_PROXY_PROTOCOL=true
# Trust Traefik's IP for PROXY protocol headers
NB_PROXY_TRUSTED_PROXIES=$TRAEFIK_IP
EOF

  if [[ "$ENABLE_CROWDSEC" == "true" && -n "$CROWDSEC_BOUNCER_KEY" ]]; then
    cat <<EOF
NB_PROXY_CROWDSEC_API_URL=http://crowdsec:8080
NB_PROXY_CROWDSEC_API_KEY=$CROWDSEC_BOUNCER_KEY
EOF
  fi

  return 0
}

render_docker_compose_traefik() {
  local network_name="${TRAEFIK_EXTERNAL_NETWORK:-netbird}"
  local network_config=""
  if [[ -n "$TRAEFIK_EXTERNAL_NETWORK" ]]; then
    network_config="    external: true"
  fi

  # Build TLS labels - certresolver is optional
  local tls_labels=""
  if [[ -n "$TRAEFIK_CERTRESOLVER" ]]; then
    tls_labels="tls.certresolver=${TRAEFIK_CERTRESOLVER}"
  fi

  cat <<EOF
services:
  # UI dashboard
  dashboard:
    image: $DASHBOARD_IMAGE
    container_name: netbird-dashboard
    restart: unless-stopped
    networks: [$network_name]
    env_file:
      - ./dashboard.env
    labels:
      - traefik.enable=true
      - traefik.http.routers.netbird-dashboard.rule=Host(\`$NETBIRD_DOMAIN\`)
      - traefik.http.routers.netbird-dashboard.entrypoints=$TRAEFIK_ENTRYPOINT
      - traefik.http.routers.netbird-dashboard.tls=true
$(if [[ -n "$tls_labels" ]]; then echo "      - traefik.http.routers.netbird-dashboard.${tls_labels}"; fi)
      - traefik.http.routers.netbird-dashboard.priority=1
      - traefik.http.services.netbird-dashboard.loadbalancer.server.port=80
    logging:
      driver: "json-file"
      options:
        max-size: "500m"
        max-file: "2"

  # Combined server (Management + Signal + Relay + STUN)
  netbird-server:
    image: $NETBIRD_SERVER_IMAGE
    container_name: netbird-server
    restart: unless-stopped
    networks: [$network_name]
    ports:
      - '$NETBIRD_STUN_PORT:$NETBIRD_STUN_PORT/udp'
    volumes:
      - ./cloink_data:/var/lib/netbird
      - ./config.yaml:/etc/netbird/config.yaml
    command: ["--config", "/etc/netbird/config.yaml"]
    labels:
      - traefik.enable=true
      # gRPC router (needs h2c backend for HTTP/2 cleartext)
      - traefik.http.routers.netbird-grpc.rule=Host(\`$NETBIRD_DOMAIN\`) && (PathPrefix(\`/signalexchange.SignalExchange/\`) || PathPrefix(\`/management.ManagementService/\`) || PathPrefix(\`/flow.FlowService/\`))
      - traefik.http.routers.netbird-grpc.entrypoints=$TRAEFIK_ENTRYPOINT
      - traefik.http.routers.netbird-grpc.tls=true
$(if [[ -n "$tls_labels" ]]; then echo "      - traefik.http.routers.netbird-grpc.${tls_labels}"; fi)
      - traefik.http.routers.netbird-grpc.service=netbird-server-h2c
      # Backend router (relay, WebSocket, log APIs, API, OAuth2)
      - traefik.http.routers.netbird-backend.rule=Host(\`$NETBIRD_DOMAIN\`) && (Path(\`/relay\`) || PathPrefix(\`/relay/\`) || PathPrefix(\`/ws-proxy/\`) || PathPrefix(\`/api/events/audit\`) || PathPrefix(\`/api/events/proxy\`) || PathPrefix(\`/api/events/network-traffic\`) || PathPrefix(\`/api\`) || PathPrefix(\`/oauth2\`))
      - traefik.http.routers.netbird-backend.entrypoints=$TRAEFIK_ENTRYPOINT
      - traefik.http.routers.netbird-backend.tls=true
$(if [[ -n "$tls_labels" ]]; then echo "      - traefik.http.routers.netbird-backend.${tls_labels}"; fi)
      - traefik.http.routers.netbird-backend.service=netbird-server
      # Services
      - traefik.http.services.netbird-server.loadbalancer.server.port=80
      - traefik.http.services.netbird-server-h2c.loadbalancer.server.port=80
      - traefik.http.services.netbird-server-h2c.loadbalancer.server.scheme=h2c
    logging:
      driver: "json-file"
      options:
        max-size: "500m"
        max-file: "2"

networks:
  $network_name:
$network_config
EOF
  return 0
}

render_docker_compose_exposed_ports() {
  local bind_addr=$(get_bind_address)
  local networks="[netbird]"
  local networks_config="networks:
  netbird:"

  # If an external network is specified, add it and include in service networks
  if [[ -n "$EXTERNAL_PROXY_NETWORK" ]]; then
    networks="[netbird, $EXTERNAL_PROXY_NETWORK]"
    networks_config="networks:
  netbird:
  $EXTERNAL_PROXY_NETWORK:
    external: true"
  fi

  cat <<EOF
services:
  # UI dashboard
  dashboard:
    image: $DASHBOARD_IMAGE
    container_name: netbird-dashboard
    restart: unless-stopped
    networks: ${networks}
    ports:
      - '${bind_addr}:${DASHBOARD_HOST_PORT}:80'
    env_file:
      - ./dashboard.env
    logging:
      driver: "json-file"
      options:
        max-size: "500m"
        max-file: "2"

  # Combined server (Management + Signal + Relay + STUN)
  netbird-server:
    image: $NETBIRD_SERVER_IMAGE
    container_name: netbird-server
    restart: unless-stopped
    networks: ${networks}
    ports:
      - '${bind_addr}:${MANAGEMENT_HOST_PORT}:80'
      - '$NETBIRD_STUN_PORT:$NETBIRD_STUN_PORT/udp'
    volumes:
      - netbird_data:/var/lib/netbird
      - ./config.yaml:/etc/netbird/config.yaml
    command: ["--config", "/etc/netbird/config.yaml"]
    logging:
      driver: "json-file"
      options:
        max-size: "500m"
        max-file: "2"

volumes:
  netbird_data:

${networks_config}
EOF
  return 0
}

render_nginx_conf() {
  local upstream_host=$(get_upstream_host)
  local dashboard_addr="${upstream_host}:${DASHBOARD_HOST_PORT}"
  local server_addr="${upstream_host}:${MANAGEMENT_HOST_PORT}"
  local install_note="# 1. Update SSL certificate paths below
# 2. Copy to your nginx config directory:
#    Debian/Ubuntu: /etc/nginx/sites-available/cloink (then symlink to sites-enabled)
#    RHEL/CentOS:   /etc/nginx/conf.d/cloink.conf
# 3. Test and reload: nginx -t && systemctl reload nginx"

  # If running in Docker network, use container names
  if [[ -n "$EXTERNAL_PROXY_NETWORK" ]]; then
    dashboard_addr="netbird-dashboard:80"
    server_addr="netbird-server:80"
    install_note="# This config uses container names since Nginx is on the same Docker network.
# Add this to your nginx.conf or include it from a separate file."
  fi

  cat <<EOF
# Cloink Nginx Configuration
# Generated by get-cloink-zh.sh
#
${install_note}

upstream netbird_dashboard {
    server ${dashboard_addr};
    keepalive 10;
}
upstream netbird_server {
    server ${server_addr};
}

server {
    listen 80;
    server_name $NETBIRD_DOMAIN;

    location / {
        return 301 https://\$host\$request_uri;
    }
}

server {
    listen 443 ssl http2;
    server_name $NETBIRD_DOMAIN;

    # SSL/TLS Configuration
    # Update these paths based on your certificate source:
    #
    # Let's Encrypt (certbot):
    #   ssl_certificate /etc/letsencrypt/live/$NETBIRD_DOMAIN/fullchain.pem;
    #   ssl_certificate_key /etc/letsencrypt/live/$NETBIRD_DOMAIN/privkey.pem;
    #
    # Let's Encrypt (acme.sh):
    #   ssl_certificate /root/.acme.sh/$NETBIRD_DOMAIN/fullchain.cer;
    #   ssl_certificate_key /root/.acme.sh/$NETBIRD_DOMAIN/$NETBIRD_DOMAIN.key;
    #
    # Custom certificates:
    #   ssl_certificate /etc/ssl/certs/$NETBIRD_DOMAIN.crt;
    #   ssl_certificate_key /etc/ssl/private/$NETBIRD_DOMAIN.key;
    #
    ssl_certificate /path/to/your/fullchain.pem;
    ssl_certificate_key /path/to/your/privkey.pem;

    # Recommended SSL settings
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_prefer_server_ciphers off;
    ssl_ciphers ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384;

    # Required for long-lived gRPC/WebSocket connections
    client_header_timeout 1d;
    client_body_timeout 1d;
    proxy_read_timeout 1d;
    proxy_send_timeout 1d;
    grpc_read_timeout 1d;
    grpc_send_timeout 1d;
    client_max_body_size 128m;

    # Common proxy headers
    proxy_set_header X-Real-IP \$remote_addr;
    proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
    proxy_set_header X-Scheme \$scheme;
    proxy_set_header X-Forwarded-Proto https;
    proxy_set_header X-Forwarded-Host \$host;
    grpc_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
    grpc_set_header X-Forwarded-Proto https;
    grpc_set_header X-Forwarded-Host \$host;

    # WebSocket connections (relay, signal, management)
    location ~ ^/(relay(?:/|$)|ws-proxy/) {
        proxy_pass http://netbird_server;
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "Upgrade";
        proxy_set_header Host \$host;
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 1d;
        proxy_send_timeout 1d;
    }

    # Native gRPC (signal + management + flow log upload)
    location ~ ^/(signalexchange\.SignalExchange|management\.ManagementService|flow\.FlowService)/ {
        grpc_pass grpc://netbird_server;
        grpc_read_timeout 1d;
        grpc_send_timeout 1d;
        grpc_socket_keepalive on;
    }

    # Dashboard log APIs (audit, reverse proxy access logs, network/DNS flow logs)
    location ~ ^/api/events/(audit|proxy|network-traffic)(/summary)?$ {
        proxy_pass http://netbird_server;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_read_timeout 300s;
        proxy_send_timeout 300s;
    }

    # HTTP routes (API + OAuth2)
    location ~ ^/(api|oauth2)/ {
        proxy_pass http://netbird_server;
        proxy_set_header Host \$host;
    }

    # Dashboard (catch-all)
    location / {
        proxy_pass http://netbird_dashboard;
    }
}
EOF
  return 0
}

render_external_caddyfile() {
  local upstream_host=$(get_upstream_host)
  local dashboard_addr="${upstream_host}:${DASHBOARD_HOST_PORT}"
  local server_addr="${upstream_host}:${MANAGEMENT_HOST_PORT}"
  local install_note="# Add this block to your existing Caddyfile and reload Caddy"

  # If running in Docker network, use container names
  if [[ -n "$EXTERNAL_PROXY_NETWORK" ]]; then
    dashboard_addr="netbird-dashboard:80"
    server_addr="netbird-server:80"
    install_note="# This config uses container names since Caddy is on the same Docker network.
# Add this block to your Caddyfile and reload Caddy."
  fi

  cat <<EOF
# Cloink Caddyfile Snippet
# Generated by get-cloink-zh.sh
#
${install_note}

$NETBIRD_DOMAIN {
    # Native gRPC (needs HTTP/2 cleartext to backend)
    @grpc header Content-Type application/grpc*
    reverse_proxy @grpc h2c://${server_addr}

    # Combined server paths (relay, WebSocket proxy, log APIs, API, OAuth2)
    @backend path /relay /relay/* /ws-proxy/* /api/events/audit* /api/events/proxy* /api/events/network-traffic* /api/* /oauth2/*
    reverse_proxy @backend ${server_addr}

    # Dashboard (everything else)
    reverse_proxy /* ${dashboard_addr}
}
EOF
  return 0
}

render_npm_advanced_config() {
  local upstream_host=$(get_upstream_host)
  local server_addr="${upstream_host}:${MANAGEMENT_HOST_PORT}"

  # If external network is specified, use container names instead of host addresses
  if [[ -n "$EXTERNAL_PROXY_NETWORK" ]]; then
    server_addr="netbird-server:80"
  fi

  cat <<EOF
# Advanced Configuration for Nginx Proxy Manager
# Paste this into the "Advanced" tab of your Proxy Host configuration
#
# 重要：必须在 SSL 选项卡启用 "HTTP/2 Support"，否则 gRPC 无法工作。

# Required for long-lived connections (gRPC and WebSocket)
client_header_timeout 1d;
client_body_timeout 1d;

# WebSocket connections (relay, signal, management)
location ~ ^/(relay(?:/|$)|ws-proxy/) {
    proxy_pass http://${server_addr};
    proxy_http_version 1.1;
    proxy_set_header Upgrade \$http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_set_header Host \$host;
    proxy_set_header X-Real-IP \$remote_addr;
    proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto \$scheme;
    proxy_read_timeout 1d;
}

# Native gRPC (signal + management + flow log upload)
location ~ ^/(signalexchange\.SignalExchange|management\.ManagementService|flow\.FlowService)/ {
    grpc_pass grpc://${server_addr};
    grpc_read_timeout 1d;
    grpc_send_timeout 1d;
    grpc_socket_keepalive on;
}

# Dashboard log APIs (audit, reverse proxy access logs, network/DNS flow logs)
location ~ ^/api/events/(audit|proxy|network-traffic)(/summary)?$ {
    proxy_pass http://${server_addr};
    proxy_set_header Host \$host;
    proxy_set_header X-Real-IP \$remote_addr;
    proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto \$scheme;
}

# HTTP routes (API + OAuth2)
location ~ ^/(api|oauth2)/ {
    proxy_pass http://${server_addr};
    proxy_set_header Host \$host;
    proxy_set_header X-Real-IP \$remote_addr;
    proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto \$scheme;
}
EOF
  return 0
}

############################################
# 按反向代理类型输出安装后说明
############################################

print_builtin_traefik_instructions() {
  echo ""
  echo "$MSG_SEPARATOR"
  echo "  CLOINK 安装完成"
  echo "$MSG_SEPARATOR"
  echo ""
  echo "你现在可以访问 Cloink 控制台："
  echo "  $NETBIRD_HTTP_PROTOCOL://$NETBIRD_DOMAIN"
  echo ""
  echo "请按照控制台引导完成 Cloink 初始化。"
  echo ""
  echo "Traefik 会通过 Let's Encrypt 自动处理 TLS 证书。"
  echo "如果看到证书警告，请稍等片刻，等待证书签发完成。"
  echo ""
  echo "需要开放的端口："
  echo "  - 443/tcp   (HTTPS - 所有 Cloink 服务)"
  echo "  - 80/tcp    (HTTP - 重定向到 HTTPS)"
  echo "  - $NETBIRD_STUN_PORT/udp   (STUN - NAT 穿透必需)"
  if [[ "$ENABLE_PROXY" == "true" ]]; then
    echo "  - 51820/udp (WIREGUARD - 可选，用于 P2P 代理连接)"
  fi
  echo ""
  echo "该部署方式适合家庭实验室和中小团队。"
  echo "如果是需要高可用和高级集成的企业环境，请评估商业本地部署许可或扩展开源部署："
  echo ""
  echo "  Commercial license: https://netbird.io/pricing#on-prem"
  echo "  Scaling guide:      https://docs.netbird.io/scaling-your-self-hosted-deployment"
  echo ""
  if [[ "$ENABLE_PROXY" == "true" ]]; then
    echo "Cloink Proxy:"
    echo "  代理服务已启用并正在运行。"
    echo "  不匹配 $NETBIRD_DOMAIN 的域名会转发到代理服务。"
    echo "  代理会通过 ACME TLS-ALPN-01 自行处理 TLS 证书。"
    echo "  请将代理域名指向本服务器域名，例如："
    echo ""
    echo "  *.$NETBIRD_DOMAIN    CNAME    $NETBIRD_DOMAIN"
    echo ""
    if [[ "$ENABLE_CROWDSEC" == "true" ]]; then
      echo "CrowdSec IP 信誉："
      echo "  CrowdSec LAPI 已运行并连接到社区拦截列表。"
      echo "  代理会自动检查客户端 IP 是否为已知威胁来源。"
      echo "  可在控制台的访问控制中按服务启用 CrowdSec。"
      echo ""
      echo "  如需接入 CrowdSec Console（可选，用于控制台和高级拦截列表）："
      echo "    docker exec netbird-crowdsec cscli console enroll <your-enrollment-key>"
      echo "  在这里获取 enrollment key：https://app.crowdsec.net"
      echo ""
    fi
  fi
  return 0
}

print_traefik_instructions() {
  echo ""
  echo "$MSG_SEPARATOR"
  echo "  TRAEFIK 配置"
  echo "$MSG_SEPARATOR"
  echo ""
  echo "Cloink 容器已配置 Traefik labels。"
  echo ""
  echo "配置："
  echo "  Entrypoint: $TRAEFIK_ENTRYPOINT"
  if [[ -n "$TRAEFIK_CERTRESOLVER" ]]; then
    echo "  证书解析器：$TRAEFIK_CERTRESOLVER"
  fi
  if [[ -n "$TRAEFIK_EXTERNAL_NETWORK" ]]; then
    echo "  网络：$TRAEFIK_EXTERNAL_NETWORK（外部）"
  else
    echo "  网络：netbird"
  fi
  echo ""
  echo "$MSG_NEXT_STEPS"
  echo "  - 确认 Traefik 已运行并完成配置"
  if [[ -n "$TRAEFIK_EXTERNAL_NETWORK" ]]; then
    echo "  - Traefik 必须加入 '$TRAEFIK_EXTERNAL_NETWORK' 网络"
  fi
  echo "  - 必须定义 entrypoint '$TRAEFIK_ENTRYPOINT'"
  if [[ -n "$TRAEFIK_CERTRESOLVER" ]]; then
    echo "  - 必须配置证书解析器 '$TRAEFIK_CERTRESOLVER'"
  fi
  echo "  - 为 gRPC 长连接关闭 entrypoint 读取超时："
  echo "    --entrypoints.$TRAEFIK_ENTRYPOINT.transport.respondingTimeouts.readTimeout=0"
  echo "  - 建议启用 HTTP 到 HTTPS 重定向"
  return 0
}

print_nginx_instructions() {
  local bind_addr=$(get_bind_address)
  echo ""
  echo "$MSG_SEPARATOR"
  echo "  NGINX 配置"
  echo "$MSG_SEPARATOR"
  echo ""
  echo "已生成：cloink-nginx.conf"
  echo ""
  echo "重要：Nginx 需要手动配置 TLS 证书。"
  echo "请先获取 SSL/TLS 证书，并在生成的配置文件中更新证书路径。"
  echo "配置文件中已包含 certbot、acme.sh 和自定义证书的示例路径。"
  echo ""
  if [[ -n "$EXTERNAL_PROXY_NETWORK" ]]; then
    echo "Cloink 容器已加入 '$EXTERNAL_PROXY_NETWORK' Docker 网络。"
    echo "配置文件会使用容器名作为 upstream。"
    echo ""
    echo "$MSG_NEXT_STEPS"
    echo "  1. 确认 Nginx 容器可以访问 SSL 证书"
    echo "     （必要时将证书目录挂载到容器中）"
    echo "  2. 编辑 cloink-nginx.conf 并更新 SSL 证书路径"
    echo "  3. 将该配置 include 到 Nginx 容器配置中"
    echo "  4. 重载 Nginx"
  else
    echo "$MSG_NEXT_STEPS"
    echo "  1. 获取 SSL/TLS 证书（推荐 Let's Encrypt）"
    echo "  2. 编辑 cloink-nginx.conf 并更新证书路径"
    echo "  3. 安装到 /etc/nginx/sites-available/（Debian）或 /etc/nginx/conf.d/（RHEL）"
    echo "  4. 测试并重载：nginx -t && systemctl reload nginx"
    echo ""
    echo "TLS 配置详情可参考："
    echo "https://docs.netbird.io/selfhosted/reverse-proxy#tls-certificate-setup-for-nginx"
    echo ""
    echo "容器端口（绑定到 ${bind_addr}）："
    echo "  控制台：      ${DASHBOARD_HOST_PORT}"
    echo "  Cloink 服务： ${MANAGEMENT_HOST_PORT}（所有服务）"
  fi
  return 0
}

print_npm_instructions() {
  local bind_addr=$(get_bind_address)
  local upstream_host=$(get_upstream_host)
  echo ""
  echo "$MSG_SEPARATOR"
  echo "  NGINX PROXY MANAGER 配置"
  echo "$MSG_SEPARATOR"
  echo ""
  echo "已生成：npm-advanced-config.txt"
  echo ""
  if [[ -n "$EXTERNAL_PROXY_NETWORK" ]]; then
    echo "Cloink 容器已加入 '$EXTERNAL_PROXY_NETWORK' Docker 网络。"
    echo ""
    echo "在 NPM 中创建 Proxy Host："
    echo "  域名：$NETBIRD_DOMAIN"
    echo "  转发主机名：netbird-dashboard"
    echo "  转发端口：80"
    echo "  Block Common Exploits：启用"
    echo ""
    echo "  SSL 选项卡："
    echo "    - 申请或选择已有证书"
    echo "    - 启用 'HTTP/2 Support'（gRPC 必需）"
    echo ""
    echo "  Advanced 选项卡："
    echo "    - 粘贴 npm-advanced-config.txt 的内容"
  else
    echo "容器端口（绑定到 ${bind_addr}）："
    echo "  控制台：      ${DASHBOARD_HOST_PORT}"
    echo "  Cloink 服务： ${MANAGEMENT_HOST_PORT}（所有服务）"
    echo ""
    echo "在 NPM 中创建 Proxy Host："
    echo "  域名：$NETBIRD_DOMAIN"
    echo "  转发主机名/IP：${upstream_host}"
    echo "  转发端口：${DASHBOARD_HOST_PORT}"
    echo "  Block Common Exploits：启用"
    echo ""
    echo "  SSL 选项卡："
    echo "    - 申请或选择已有证书"
    echo "    - 启用 'HTTP/2 Support'（gRPC 必需）"
    echo ""
    echo "  Advanced 选项卡："
    echo "    - 粘贴 npm-advanced-config.txt 的内容"
  fi
  return 0
}

print_external_caddy_instructions() {
  local bind_addr=$(get_bind_address)
  echo ""
  echo "$MSG_SEPARATOR"
  echo "  外部 CADDY 配置"
  echo "$MSG_SEPARATOR"
  echo ""
  echo "已生成：caddyfile-cloink.txt"
  echo ""
  if [[ -n "$EXTERNAL_PROXY_NETWORK" ]]; then
    echo "Cloink 容器已加入 '$EXTERNAL_PROXY_NETWORK' Docker 网络。"
    echo "配置使用容器名作为 upstream。"
    echo ""
    echo "$MSG_NEXT_STEPS"
    echo "  1. 将 caddyfile-cloink.txt 的内容加入你的 Caddyfile"
    echo "  2. 重载 Caddy"
  else
    echo "$MSG_NEXT_STEPS"
    echo "  1. 将 caddyfile-cloink.txt 的内容加入你的 Caddyfile"
    echo "  2. 重载 Caddy：caddy reload --config /path/to/Caddyfile"
    echo ""
    echo "容器端口（绑定到 ${bind_addr}）："
    echo "  控制台：      ${DASHBOARD_HOST_PORT}"
    echo "  Cloink 服务： ${MANAGEMENT_HOST_PORT}（所有服务）"
  fi
  return 0
}

print_manual_instructions() {
  local bind_addr=$(get_bind_address)
  local upstream_host=$(get_upstream_host)
  echo ""
  echo "$MSG_SEPARATOR"
  echo "  手动反向代理配置"
  echo "$MSG_SEPARATOR"
  echo ""
  echo "容器端口（绑定到 ${bind_addr}）："
  echo "  控制台：      ${DASHBOARD_HOST_PORT}"
  echo "  Cloink 服务： ${MANAGEMENT_HOST_PORT}（management、signal、relay 等所有服务）"
  echo ""
  echo "请在反向代理中配置这些路由（全部指向同一个后端）："
  echo ""
  echo "  WebSocket（relay、signal、management WS proxy）："
  echo "    /relay, /relay/*, /ws-proxy/*  -> ${upstream_host}:${MANAGEMENT_HOST_PORT}"
  echo "    （HTTP + WebSocket upgrade，需要较长超时）"
  echo ""
  echo "  原生 gRPC（signal + management + 流日志上传）："
  echo "    /signalexchange.SignalExchange/* -> ${upstream_host}:${MANAGEMENT_HOST_PORT}"
  echo "    /management.ManagementService/* -> ${upstream_host}:${MANAGEMENT_HOST_PORT}"
  echo "    /flow.FlowService/*            -> ${upstream_host}:${MANAGEMENT_HOST_PORT}"
  echo "    （gRPC/h2c，明文 HTTP/2 upstream）"
  echo ""
  echo "  HTTP（API + 内置 IdP）："
  echo "    /api/events/audit*             -> ${upstream_host}:${MANAGEMENT_HOST_PORT}"
  echo "    /api/events/proxy*             -> ${upstream_host}:${MANAGEMENT_HOST_PORT}"
  echo "    /api/events/network-traffic*   -> ${upstream_host}:${MANAGEMENT_HOST_PORT}"
  echo "    /api/*, /oauth2/*              -> ${upstream_host}:${MANAGEMENT_HOST_PORT}"
  echo ""
  echo "  控制台（兜底路由）："
  echo "    /*                             -> ${upstream_host}:${DASHBOARD_HOST_PORT}"
  echo ""
  echo "重要：gRPC 路由需要 upstream 支持 HTTP/2（h2c）。"
  echo "WebSocket 和 gRPC 连接需要较长超时，建议 1 天。"
  return 0
}

print_post_setup_instructions() {
  case "$REVERSE_PROXY_TYPE" in
    0)
      print_builtin_traefik_instructions
      ;;
    1)
      print_traefik_instructions
      ;;
    2)
      print_nginx_instructions
      ;;
    3)
      print_npm_instructions
      ;;
    4)
      print_external_caddy_instructions
      ;;
    5)
      print_manual_instructions
      ;;
    *)
      echo "Unknown reverse proxy type: $REVERSE_PROXY_TYPE" > /dev/stderr
      ;;
  esac
  return 0
}

init_environment
