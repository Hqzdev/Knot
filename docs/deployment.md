# Kubernetes deployment

The Kubernetes layer runs every Knot application as a stateless workload and keeps PostgreSQL, Redis, NATS JetStream, S3, and the OTLP collector external to the namespace. The only public workload is the Web proxy. It serves the application and forwards `/api`, `/gateway`, `/presence`, `/attachments`, and `/push` to private ClusterIP services, preserving a single browser origin.

## Requirements

- Kubernetes 1.27 or newer
- A CNI that enforces `NetworkPolicy`
- ingress-nginx for the supplied overlay
- Metrics Server for Gateway autoscaling
- External PostgreSQL, Redis, NATS JetStream, S3-compatible storage, and OTLP endpoints
- Eight published container images under `ghcr.io/hqzdev`

The base references these images:

| Workload | Image |
| --- | --- |
| API | `ghcr.io/hqzdev/knot-api:main` |
| Presence | `ghcr.io/hqzdev/knot-presence:main` |
| Attachments | `ghcr.io/hqzdev/knot-attachments:main` |
| Push | `ghcr.io/hqzdev/knot-push:main` |
| Delivery | `ghcr.io/hqzdev/knot-delivery:main` |
| Router | `ghcr.io/hqzdev/knot-router:main` |
| Gateway | `ghcr.io/hqzdev/knot-gateway:main` |
| Web | `ghcr.io/hqzdev/knot-web:main` |

Production overlays should replace mutable tags with immutable image digests. Private registries also require an `imagePullSecrets` patch.

## Runtime configuration contract

The manifests intentionally do not create `knot-runtime-config`, `knot-runtime-secrets`, or `knot-tls`. Provision all three in the `knot` namespace through the deployment platform or secret manager before applying the workloads.

`knot-runtime-config` must contain:

| Key | Required value |
| --- | --- |
| `KNOT_CORS_ORIGINS` | Comma-separated public HTTPS origins accepted by API, Presence, Attachments, Push, and Gateway |
| `KNOT_S3_ENDPOINT` | Private S3 API endpoint reachable from Attachments |
| `KNOT_S3_PUBLIC_ENDPOINT` | Browser-reachable S3 endpoint used in signed URLs |
| `KNOT_S3_REGION` | S3 signing region |
| `KNOT_S3_BUCKET` | Existing ciphertext object bucket |
| `KNOT_S3_PATH_STYLE` | `true` for path-style S3 or `false` for virtual-host style |
| `KNOT_PUSH_PROVIDER_MODE` | `production` |
| `KNOT_APNS_KEY_ID` | Ten-character Apple key identifier |
| `KNOT_APNS_TEAM_ID` | Ten-character Apple team identifier |
| `KNOT_APNS_TOPIC` | Apple bundle identifier |
| `KNOT_APNS_ENVIRONMENT` | `production` or `sandbox` |
| `KNOT_WEB_PUSH_VAPID_PUBLIC_KEY` | P-256 VAPID public key |
| `KNOT_WEB_PUSH_SUBJECT` | HTTPS URL or `mailto:` contact |
| `KNOT_NATS_REPLICAS` | JetStream replica count from 1 through 5 |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTLP gRPC or HTTP endpoint |

`knot-runtime-secrets` must contain:

| Key | Consumer |
| --- | --- |
| `KNOT_DATABASE_URL` | API, Push, Router |
| `KNOT_ATTACHMENTS_DATABASE_URL` | Attachments |
| `KNOT_DELIVERY_DATABASE_URL` | Delivery PostgreSQL fallback |
| `KNOT_REDIS_URL` | API, Presence, Router, Gateway |
| `KNOT_NATS_URL` | Delivery JetStream |
| `KNOT_JWT_SECRET` | API, Presence, Attachments, Push, Gateway; at least 32 bytes |
| `KNOT_PUSH_INTERNAL_TOKEN` | API, Router, Push; at least 32 bytes |
| `KNOT_INTERNAL_GRPC_TOKEN` | Gateway, Router, Delivery; at least 32 bytes |
| `KNOT_DELIVERY_TOKEN_SECRET` | Delivery cursor and acknowledgement signing; at least 32 bytes |
| `KNOT_S3_ACCESS_KEY_ID` | Attachments |
| `KNOT_S3_SECRET_ACCESS_KEY` | Attachments |
| `KNOT_S3_SESSION_TOKEN` | Attachments when temporary S3 credentials are used |
| `KNOT_WEB_PUSH_VAPID_PRIVATE_KEY` | Push |
| `KNOT_APNS_PRIVATE_KEY` | Push, stored as the original PKCS#8 `.p8` file contents |

Database, Redis, and NATS URLs should require transport encryption and certificate verification. Give each database identity only the schema privileges its service requires. S3 credentials should be restricted to the configured bucket and the object operations used by Attachments.

Create the runtime objects from protected files without committing them:

```sh
kubectl apply -f deploy/kubernetes/base/namespace.yaml
kubectl -n knot create configmap knot-runtime-config --from-env-file="$KNOT_RUNTIME_CONFIG_FILE" --dry-run=client -o yaml | kubectl apply -f -
kubectl -n knot create secret generic knot-runtime-secrets --from-env-file="$KNOT_RUNTIME_SECRETS_FILE" --from-file=KNOT_APNS_PRIVATE_KEY="$KNOT_APNS_PRIVATE_KEY_FILE" --dry-run=client -o yaml | kubectl apply -f -
kubectl -n knot create secret tls knot-tls --cert="$KNOT_TLS_CERT_FILE" --key="$KNOT_TLS_KEY_FILE" --dry-run=client -o yaml | kubectl apply -f -
```

The TLS certificate must cover the public hostname routed to ingress-nginx. Set `KNOT_CORS_ORIGINS` to the exact HTTPS origin, including a non-default port when one is used. The supplied Ingress is hostless so it can be used with any hostname; add a host rule in an environment overlay when the ingress controller serves multiple applications.

## Validation and rollout

Render and inspect the complete object graph before applying it:

```sh
kubectl kustomize deploy/kubernetes/overlays/ingress-nginx
kubectl apply --server-side --dry-run=server -k deploy/kubernetes/overlays/ingress-nginx
kubectl apply -k deploy/kubernetes/overlays/ingress-nginx
kubectl -n knot rollout status deployment/api
kubectl -n knot rollout status deployment/presence
kubectl -n knot rollout status deployment/attachments
kubectl -n knot rollout status deployment/push
kubectl -n knot rollout status deployment/delivery
kubectl -n knot rollout status deployment/router
kubectl -n knot rollout status deployment/gateway
kubectl -n knot rollout status deployment/web
kubectl -n knot get pods,services,ingress,hpa,pdb,networkpolicy
```

Environment-variable and projected-key changes require a rollout because running processes do not reload them:

```sh
kubectl -n knot rollout restart deployment/api deployment/presence deployment/attachments deployment/push deployment/delivery deployment/router deployment/gateway
```

## Network boundaries

The base applies namespace-wide default-deny ingress and egress. It then permits only the Web-to-service paths, internal API-to-Push calls, Gateway-to-Router/Delivery gRPC, Router-to-Delivery/Push calls, and DNS to `kube-system` on both UDP and TCP port 53. The ingress overlay permits only standard ingress-nginx pods to reach Web.

External dependency egress initially permits IPv4 and IPv6 destinations on these ports:

| Dependency | Ports |
| --- | --- |
| S3, APNs, Web Push | TCP 443 |
| PostgreSQL | TCP 5432 |
| Redis | TCP 6379 and 6380 |
| NATS | TCP 4222 |
| OTLP | TCP 4317 and 4318 |

Before production rollout, narrow every external `ipBlock` to the provider CIDRs. If a provider rotates addresses dynamically, use the CNI's authenticated FQDN or service-aware egress policy instead. Patch the port lists when an external dependency uses a non-standard port. Clusters with a differently named ingress namespace or differently labeled ingress controller must patch `allow-ingress-nginx-to-web` accordingly.

All Pods enforce the Restricted Pod Security Standard, run without service-account tokens, drop Linux capabilities, use read-only root filesystems, and expose health probes only on pod-local service ports. Gateway scales from two through twelve replicas and permits at most one replica to be unavailable during voluntary disruptions.
