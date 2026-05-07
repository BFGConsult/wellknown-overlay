#!/bin/sh
set -eu

OVERLAY_CONFIG="${OVERLAY_CONFIG:-/etc/wellknown-overlay/overlay.json}"
OVERLAY_ROOT="${OVERLAY_ROOT:-/var/lib/wellknown-overlay/public}"
OVERLAY_LISTEN="${OVERLAY_LISTEN:-127.0.0.1:8765}"
OVERLAY_UPSTREAM="http://${OVERLAY_LISTEN}"
BACKEND_URL="${BACKEND_URL:-}"

cat >/etc/nginx/conf.d/default.conf <<EOF
server {
    listen 80;
    server_name _;

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

    location = /Autodiscover/Autodiscover.xml {
        proxy_pass ${OVERLAY_UPSTREAM};
    }
EOF

if [ -n "$BACKEND_URL" ]; then
    cat >>/etc/nginx/conf.d/default.conf <<EOF

    location / {
        proxy_pass ${BACKEND_URL};
    }
EOF
else
    cat >>/etc/nginx/conf.d/default.conf <<'EOF'

    location / {
        root /usr/share/wellknown-overlay/placeholder;
        try_files $uri $uri/ /index.html;
    }
EOF
fi

cat >>/etc/nginx/conf.d/default.conf <<'EOF'
}
EOF

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
