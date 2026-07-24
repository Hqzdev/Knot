# Knot Unsecure Attachments

Attachments stores source file bytes through bounded S3-compatible upload requests. The service records size and SHA-256, verifies the completed object byte count and returns publicly accessible HTTP download metadata.

Creation and completion require a signed Knot session. Download metadata and profile media are public. Upload size, request-body size, pending TTL and cleanup work remain bounded.

Ready objects receive no application expiration. Pending uploads expire and are cleaned up. The Compose MinIO bucket permits anonymous downloads.
