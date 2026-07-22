# Knot Attachments Service

The service stores metadata for opaque client-encrypted blobs and issues bounded S3-compatible SigV4 requests. It never accepts filenames, MIME types, plaintext, encryption keys, or media keys.

## API

All `/v1/attachments` routes require `Authorization: Bearer <access_token>`. Tokens use the Knot HS256 contract and must contain signed `sub`, `device_id`, and `exp` claims.

`POST /v1/attachments` accepts:

```json
{"ciphertext_size":42,"ciphertext_sha256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}
```

It returns `201` with `attachment_id`, `upload_url`, `upload_expires_at`, `required_headers`, `ciphertext_size`, and `ciphertext_sha256`. The client must PUT exactly the declared number of ciphertext bytes and include every required signed header.

Browser runtimes set `Content-Length` from the Blob automatically and must upload a Blob of the declared size; they should explicitly set `X-Amz-Meta-Sha256`. The metadata row remains pending beyond the PUT URL lifetime so an upload that started before URL expiry can finish and be completed safely.

`POST /v1/attachments/{attachment_id}/complete` has an empty body. Only the creating user can complete the upload. The service performs an authenticated S3 HEAD and requires exact `Content-Length` and `X-Amz-Meta-Sha256` matches before returning the ready metadata.

`GET /v1/attachments/{attachment_id}` returns a bounded `download_url` to any authenticated holder of the unguessable 192-bit attachment ID. Pending, expired, deleting, and unknown attachments return `404`.

`DELETE /v1/attachments/{attachment_id}` is owner-user only and returns `204`. Deletion first makes metadata unavailable, then deletes the object and purges the tombstone. The cleanup worker retries failed object deletions.

`GET /healthz` is process liveness. `GET /readyz` verifies both metadata and object-store readiness.

## Environment

- `KNOT_ATTACHMENTS_ADDRESS`, default `:8082`
- `KNOT_JWT_SECRET`, required
- `KNOT_ATTACHMENTS_STORE`, `postgres` by default or explicit `memory`
- `KNOT_ATTACHMENTS_DATABASE_URL`, required for Postgres
- `KNOT_S3_ENDPOINT`, required and reachable by the service
- `KNOT_S3_PUBLIC_ENDPOINT`, optional externally reachable origin for presigned client URLs
- `KNOT_S3_REGION`, default `us-east-1`
- `KNOT_S3_BUCKET`, required
- `KNOT_S3_ACCESS_KEY_ID`, required
- `KNOT_S3_SECRET_ACCESS_KEY`, required
- `KNOT_S3_SESSION_TOKEN`, optional
- `KNOT_S3_PATH_STYLE`, default `true`
- `KNOT_ATTACHMENT_MAX_SIZE_BYTES`, default 100 MiB, range 1 byte through 5 GiB
- `KNOT_ATTACHMENT_UPLOAD_TTL`, default 15 minutes, range 1 minute through 1 hour
- `KNOT_ATTACHMENT_PENDING_TTL`, default 24 hours, range 1 hour through 7 days and never shorter than upload TTL
- `KNOT_ATTACHMENT_DOWNLOAD_TTL`, default 5 minutes, range 1 minute through 1 hour
- `KNOT_ATTACHMENT_RETENTION_TTL`, default 7 days, range 1 hour through 365 days
- `KNOT_ATTACHMENT_CLEANUP_INTERVAL`, default 1 minute, range 10 seconds through 1 hour
- `KNOT_ATTACHMENT_CLEANUP_BATCH`, default 100, range 1 through 1000

The built-in SigV4 provider supports explicitly configured static credentials and an optional session token. It does not perform AWS profile, environment-chain, EC2/ECS metadata, web-identity, or role-assumption credential discovery. Rotate credentials outside the service and restart it after rotation.

The bucket must already exist and remain private. Credentials need bucket readiness permission plus `PutObject`, `GetObject`, and `DeleteObject` only for the configured bucket and attachment prefix; object listing is not used. Browser uploads require bucket CORS to allow the application origins, `PUT` and `GET`, and `X-Amz-Meta-Sha256`. A defensive bucket lifecycle expiration longer than the configured service retention is recommended for objects orphaned by unrecoverable metadata-store loss.

## Verification

```sh
go test ./services/attachments/...
go test -race ./services/attachments/...
go vet ./services/attachments/...
```

Set `KNOT_TEST_ATTACHMENTS_DATABASE_URL` to include the conditional Postgres migration and lifecycle test.
