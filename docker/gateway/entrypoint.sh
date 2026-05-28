#!/bin/sh
set -eu

OVERLAY_CONFIG="${OVERLAY_CONFIG:-/etc/wellknown-overlay/overlay.json}"
OVERLAY_ROOT="${OVERLAY_ROOT:-/var/lib/wellknown-overlay/public}"
OVERLAY_LISTEN="${OVERLAY_LISTEN:-127.0.0.1:8765}"
OVERLAY_UPSTREAM="http://${OVERLAY_LISTEN}"
BACKEND_URL="${BACKEND_URL:-}"
BACKEND_HOSTS="${BACKEND_HOSTS:-}"
OVERLAY_ONLY_HOSTS="${OVERLAY_ONLY_HOSTS:-}"
NGINX_CONFIG="/etc/nginx/conf.d/default.conf"
PLACEHOLDER_ROOT="/usr/share/wellknown-overlay/placeholder"

wellknown-overlay-docker-entrypoint prepare-config -config "$OVERLAY_CONFIG"

words_from_csv() {
    printf '%s' "$1" | tr ',' ' '
}

has_star() {
    for host in $(words_from_csv "$1"); do
        if [ "$host" = "*" ]; then
            return 0
        fi
    done
    return 1
}

server_names_from_csv() {
    names=""
    for host in $(words_from_csv "$1"); do
        if [ -n "$host" ] && [ "$host" != "*" ]; then
            names="${names}${names:+ }${host}"
        fi
    done
    printf '%s' "$names"
}

fail_config() {
    echo "wellknown-overlay gateway config error: $*" >&2
    exit 1
}

validate_host_config() {
    if [ -n "$BACKEND_HOSTS" ] && [ -z "$BACKEND_URL" ]; then
        fail_config "BACKEND_HOSTS requires BACKEND_URL"
    fi

    if has_star "$BACKEND_HOSTS" && has_star "$OVERLAY_ONLY_HOSTS"; then
        fail_config "BACKEND_HOSTS and OVERLAY_ONLY_HOSTS cannot both contain *"
    fi

    for backend_host in $(words_from_csv "$BACKEND_HOSTS"); do
        [ "$backend_host" = "*" ] && continue
        for overlay_host in $(words_from_csv "$OVERLAY_ONLY_HOSTS"); do
            [ "$overlay_host" = "*" ] && continue
            if [ "$backend_host" = "$overlay_host" ]; then
                fail_config "host $backend_host is listed in both BACKEND_HOSTS and OVERLAY_ONLY_HOSTS"
            fi
        done
    done
}

write_overlay_locations() {
    cat >>"$NGINX_CONFIG" <<EOF
    location = /healthz {
        proxy_pass ${OVERLAY_UPSTREAM};
    }

    location ^~ /.well-known/autoconfig/ {
        proxy_pass ${OVERLAY_UPSTREAM};
    }

    location ^~ /.well-known/openpgpkey/ {
        proxy_pass ${OVERLAY_UPSTREAM};
    }

    location = /.well-known/mail/apple.mobileconfig {
        proxy_pass ${OVERLAY_UPSTREAM};
    }

    location = /.well-known/security.txt {
        proxy_pass ${OVERLAY_UPSTREAM};
    }

    location = /.well-known/mta-sts.txt {
        proxy_pass ${OVERLAY_UPSTREAM};
    }

    location = /mail/config-v1.1.xml {
        proxy_pass ${OVERLAY_UPSTREAM};
    }

    location = /mail/setup {
        proxy_pass ${OVERLAY_UPSTREAM};
    }

    location = /mail/setup-og.png {
        proxy_pass ${OVERLAY_UPSTREAM};
    }

    location = /Autodiscover/Autodiscover.xml {
        proxy_pass ${OVERLAY_UPSTREAM};
    }

    location = /AutoDiscover/AutoDiscover.xml {
        proxy_pass ${OVERLAY_UPSTREAM};
    }

    location = /autodiscover/autodiscover.xml {
        proxy_pass ${OVERLAY_UPSTREAM};
    }
EOF
}

write_backend_fallback() {
    cat >>"$NGINX_CONFIG" <<EOF

    location / {
        proxy_pass ${BACKEND_URL};
    }
EOF
}

write_placeholder_fallback() {
    cat >>"$NGINX_CONFIG" <<EOF

    location / {
        root ${PLACEHOLDER_ROOT};
        try_files \$uri \$uri/ /index.html;
    }
EOF
}

write_server() {
    listen_directive="$1"
    server_names="$2"
    fallback="$3"

    cat >>"$NGINX_CONFIG" <<EOF
server {
    ${listen_directive}
    server_name ${server_names};

EOF
    write_overlay_locations
    if [ "$fallback" = "backend" ]; then
        write_backend_fallback
    else
        write_placeholder_fallback
    fi
    cat >>"$NGINX_CONFIG" <<'EOF'
}
EOF
}

write_default_404() {
    cat >>"$NGINX_CONFIG" <<EOF
server {
    listen 80 default_server;
    server_name _;

    location = /healthz {
        proxy_pass ${OVERLAY_UPSTREAM};
    }

    location / {
        return 404;
    }
}
EOF
}

: >"$NGINX_CONFIG"

if [ -n "$BACKEND_HOSTS$OVERLAY_ONLY_HOSTS" ]; then
    validate_host_config

    backend_names="$(server_names_from_csv "$BACKEND_HOSTS")"
    overlay_names="$(server_names_from_csv "$OVERLAY_ONLY_HOSTS")"

    if [ -n "$backend_names" ]; then
        write_server "listen 80;" "$backend_names" "backend"
    fi

    if [ -n "$overlay_names" ]; then
        write_server "listen 80;" "$overlay_names" "placeholder"
    fi

    if has_star "$BACKEND_HOSTS"; then
        write_server "listen 80 default_server;" "_" "backend"
    elif has_star "$OVERLAY_ONLY_HOSTS"; then
        write_server "listen 80 default_server;" "_" "placeholder"
    else
        write_default_404
    fi
elif [ -n "$BACKEND_URL" ]; then
    write_server "listen 80;" "_" "backend"
else
    write_server "listen 80;" "_" "placeholder"
fi

wellknown-overlay serve -config "$OVERLAY_CONFIG" -root "$OVERLAY_ROOT" -listen "$OVERLAY_LISTEN" &
overlay_pid="$!"

nginx -g "daemon off;" &
nginx_pid="$!"

term() {
    kill "$overlay_pid" "$nginx_pid" 2>/dev/null || true
}
trap term INT TERM

while :; do
    if ! kill -0 "$overlay_pid" 2>/dev/null; then
        term
        wait "$nginx_pid" 2>/dev/null || true
        wait "$overlay_pid" 2>/dev/null || true
        exit 1
    fi

    if ! kill -0 "$nginx_pid" 2>/dev/null; then
        term
        wait "$overlay_pid" 2>/dev/null || true
        wait "$nginx_pid" 2>/dev/null || true
        exit 1
    fi

    sleep 1
done
