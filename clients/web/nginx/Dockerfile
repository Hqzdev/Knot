FROM nginxinc/nginx-unprivileged:1.29-alpine

ENV KNOT_WEB_UPSTREAM=http://web-app:3000
ENV KNOT_API_UPSTREAM=http://api:8080
ENV KNOT_GATEWAY_UPSTREAM=http://gateway:8086
ENV KNOT_PRESENCE_UPSTREAM=http://presence:8083
ENV KNOT_ATTACHMENTS_UPSTREAM=http://attachments:8082
ENV KNOT_PREVIEW_UPSTREAM=http://preview:8087

COPY clients/web/nginx/default.conf.template /etc/nginx/templates/default.conf.template
COPY clients/web/nginx/proxy.conf /etc/nginx/snippets/knot-proxy.conf

EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 CMD wget -q -O - http://127.0.0.1:8080/healthz || exit 1
