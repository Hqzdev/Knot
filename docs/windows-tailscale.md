# Windows and Tailscale server

This deployment keeps Knot private to devices in one Tailscale network while providing the HTTPS and WSS origins required by browsers and Apple platforms.

## Result

The installer exposes two endpoints:

- `https://<windows-device>.<tailnet>.ts.net` for Web, API, Gateway, Presence, Attachments metadata, and Push registration.
- `https://<windows-device>.<tailnet>.ts.net:8443` for encrypted MinIO objects.

All Docker ports bind to Windows loopback. Tailscale is the only network ingress. PostgreSQL, NATS, and MinIO data remain in named Docker volumes, and long-running containers use `restart: unless-stopped`.

## Prerequisites

Use Windows 10 or Windows 11 with hardware virtualization enabled. Docker Desktop does not support Windows Server editions.

Install WSL2, Docker Desktop, and Tailscale from an elevated PowerShell window:

```powershell
wsl --install
winget install --exact --id Docker.DockerDesktop
winget install --exact --id Tailscale.Tailscale
```

Restart Windows when requested. Enable these settings:

1. Docker Desktop: `Use the WSL 2 based engine`.
2. Docker Desktop: `Start Docker Desktop when you sign in`.
3. Tailscale: sign in to the same tailnet used by the iPhone and Mac.
4. Tailscale admin console: enable MagicDNS and HTTPS certificates.

Clone Knot into a short Windows path such as `C:\Knot`, then open Windows PowerShell in that repository root. WSL2 runs the Linux containers, while the installer uses the Windows Docker and Tailscale commands.

## Installation

Run:

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\deploy\windows\Install-KnotServer.ps1
```

The installer performs these operations without replacing existing secrets:

1. Discovers the Windows device Tailscale DNS name.
2. Creates or updates the ignored root `.env` file.
3. Generates missing server secrets with a cryptographic random generator.
4. Builds and starts the complete Docker Compose stack.
5. Publishes Web on Tailscale HTTPS port `443`.
6. Publishes MinIO on Tailscale HTTPS port `8443`.
7. Waits for both public health endpoints.

Tailscale may open a one-time approval page when HTTPS is enabled for the first time.

## Apple clients

The installer prints the unified Apple build setting:

```text
KNOT_SERVER_BASE_URL=https://<windows-device>.<tailnet>.ts.net
```

On the Mac, open the Knot target Build Settings in Xcode and change the user-defined `KNOT_SERVER_BASE_URL` value for Debug and Release to the printed HTTPS URL. Build and install the app again on every iPhone or Mac. The value is embedded in the installed application and remains active when the app is launched outside Xcode.

The application derives all service routes from that origin:

| Client service | URL |
| --- | --- |
| API | `<server>/api/v1/...` |
| Gateway | `wss://<server>/gateway/v1/gateway/ws` |
| Attachments | `<server>/attachments/v1/...` |
| Push registration | `<server>/push/v1/...` |

Install Tailscale on each Apple device, sign in to the same tailnet, and verify the Windows device is reachable before opening Knot.

## Web client

Open the HTTPS URL printed by the installer. Do not use the Windows LAN IP or `localhost`. The HTTPS origin keeps WebCrypto available and works from any network while Tailscale is connected.

## Operations

Start an existing installation:

```powershell
.\deploy\windows\Start-KnotServer.ps1
```

Verify containers and public HTTPS endpoints:

```powershell
.\deploy\windows\Test-KnotServer.ps1
```

Inspect service failures:

```powershell
docker compose ps
docker compose logs --tail 200 api gateway web attachments
tailscale serve status
```

Docker restarts the containers when Docker Desktop starts. Tailscale Serve configurations created with `--bg` remain active across Tailscale and operating-system restarts.

## Updates

After pulling a new revision, run the installer again. Existing secrets and named volumes are preserved.

## Push notifications

Realtime delivery works while Knot is running. Background APNs and Web Push remain disabled until Apple APNs credentials and Web Push VAPID keys are supplied. Never commit `.p8`, `.env`, or generated private keys.

## Public deployment

Tailscale Serve is private to the tailnet. If Knot must be reachable by users who cannot join it, deploy the same Compose stack to an Ubuntu VPS and place Caddy in front of Web and MinIO with a real domain. Do not expose PostgreSQL, Redis, NATS, or internal Go service ports directly.
