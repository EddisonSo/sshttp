# sshttp - Secure Shell over HTTP

Interactive shell access over HTTPS using a browser and FIDO2/WebAuthn authentication.

## Motivation

SSH is ubiquitous but operationally annoying:

- Every client behaves differently (OpenSSH, PuTTY, WSL, VS Code, etc.)
- Agents, sockets, forwarding, and keychain quirks are brittle
- Hardware keys work, but UX differs per platform/client

sshttp makes shell access feel like modern login:

1. Open a URL
2. Touch a security key or use biometrics
3. Shell opens

## Features

- **Passwordless Authentication**: FIDO2/WebAuthn with YubiKey + platform authenticators
- **Cross-Platform**: Consistent UX on Windows, macOS, and Linux via browser or Electron
- **Web Terminal**: Full terminal emulation using xterm.js with WebGL rendering
- **Real-time PTY**: WebSocket-based PTY streaming with resize support
- **Single Port**: HTTPS only (443)
- **No Local Keys**: No SSH agent or key files required
- **Security Hardened**: Rate limiting, CORS, CSP headers, JWT tokens, audit logging

## Quick Start

### Prerequisites

- Go 1.24+
- Node.js 18+
- A WebAuthn-compatible browser and authenticator (YubiKey, Touch ID, Windows Hello, etc.)

### Build

```bash
# Build server
cd server
go build -o sshttpd ./cmd/sshttpd

# Build client
cd ../client
npm install
npm run build
```

### Run

1. Start the server:
```bash
cd server
./sshttpd
```

2. Create a registration link for a user:
```bash
./sshttpd --register <username>
```

This outputs a one-time URL like:
```
https://sshttp.example.com/register?rid=abc-123-abc-123
```

The link is:
- Random and unguessable (128+ bits of entropy)
- Short-lived (10 minute TTL)
- Single-use

3. Start the frontend dev server (development only):
```bash
cd client
npm run dev
```

4. Open the registration link in your browser and complete WebAuthn registration.

5. Navigate to `/login` and authenticate with your passkey.

## Configuration

Environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `SSHTTP_ADDR` | `:4422` | Server listen address |
| `SSHTTP_DATA_DIR` | `~/.sshttp` | Data directory (database, secrets) |
| `SSHTTP_TLS_CERT` | `$DATA_DIR/cert.pem` | TLS certificate path |
| `SSHTTP_TLS_KEY` | `$DATA_DIR/key.pem` | TLS private key path |
| `SSHTTP_RP_ID` | `localhost` | WebAuthn Relying Party ID |
| `SSHTTP_RP_ORIGIN` | `http://localhost:4422` | Allowed origin for WebAuthn |
| `SSHTTP_JWT_SECRET` | (auto-generated) | JWT signing secret |
| `SSHTTP_TOKEN_EXPIRY_MINS` | `15` | JWT token expiry in minutes |
| `SSHTTP_SESSION_IDLE_TIMEOUT_MINS` | `30` | Shell session idle timeout |

**TLS is required** for WebAuthn authentication to work. TLS is enabled automatically if both cert and key files exist at the configured paths.

All secrets and data (database, JWT secret, certificates) are stored in `~/.sshttp` by default, outside the repository.

## Architecture

```
Client (Browser/Electron)  <— HTTPS —>  sshttpd  — local —>  Shell
```

### Components

**Client**
- WebAuthn API for login/registration
- Terminal UI (xterm.js)
- WebSocket stream for I/O

**sshttpd**
- WebAuthn verification
- Session/token issuance
- PTY spawn + I/O pumps
- Authorization/policy checks
- Logging/auditing

### Directory Structure

```
sshttp/
├── server/                    # Go backend
│   ├── cmd/sshttpd/          # Main entry point
│   └── internal/
│       ├── api/              # HTTP handlers
│       ├── auth/             # WebAuthn + JWT
│       ├── config/           # Configuration
│       ├── middleware/       # HTTP middleware
│       ├── pty/              # PTY session manager
│       └── store/            # SQLite storage
└── client/                   # React frontend
    └── src/
        ├── components/       # React components
        ├── hooks/            # Custom hooks
        ├── lib/              # Utilities
        └── pages/            # Page components
```

## API

### Registration (one-time link)

| Endpoint | Description |
|----------|-------------|
| `GET /register?rid=...` | Serves registration UI (only if rid valid) |
| `POST /v1/register/begin` | Returns `PublicKeyCredentialCreationOptions` |
| `POST /v1/register/finish` | Verifies attestation, stores credential, invalidates rid |

### Authentication

| Endpoint | Description |
|----------|-------------|
| `POST /v1/auth/begin` | Returns `PublicKeyCredentialRequestOptions` + state |
| `POST /v1/auth/finish` | Verifies assertion, returns access token |

### Shell

| Endpoint | Description |
|----------|-------------|
| `POST /v1/shell/open` | Creates session ID (optional) |
| `GET /v1/shell/stream` | WebSocket endpoint for PTY streaming |

## WebSocket Protocol

Binary frames with type prefix:

| Type | Value | Direction | Payload |
|------|-------|-----------|---------|
| STDIN | `0x01` | Client → Server | Terminal input bytes |
| STDOUT | `0x02` | Server → Client | Terminal output bytes |
| RESIZE | `0x04` | Client → Server | cols:u16, rows:u16 (big endian) |
| EXIT | `0x05` | Server → Client | exit_code:u32 (big endian) |
| FILE_START | `0x10` | Client → Server | size:u32, name_len:u16, name:utf8 |
| FILE_CHUNK | `0x11` | Client → Server | offset:u32, data:bytes |
| FILE_ACK | `0x12` | Server → Client | status:u8, message?:utf8 |

### File Transfer

Files can be uploaded by dragging and dropping onto the terminal. Files are transferred to the shell's current working directory.

**FILE_ACK Status Codes:**
- `0x00` - Success (transfer complete)
- `0x01` - Progress (chunk received)
- `0x02` - Error (message contains error description)

**Limits:**
- Maximum file size: 100MB
- Chunk size: 32KB
- Files starting with `.` or containing `/`, `\`, `..` are rejected
- Existing files will not be overwritten

## Identity Model

Registration stores per-user credentials:

- `credentialId`: Unique identifier for the credential
- `publicKey`: From WebAuthn attestation (COSE format)
- `signCount`: For replay detection
- Optional metadata: AAGUID, transports, label ("YubiKey USB-C")

Multiple credentials per user are supported (primary + backup keys).

## Security

- **TLS mandatory** for production
- Strict origin + rpIdHash verification
- Challenge single-use with short TTL
- Rate limiting on auth/register endpoints
- CSP + XSS hardening on terminal UI
- Drop privileges / sandbox after PTY spawn
- Audit logs: login events, credential used, session start/stop
- JWT tokens with configurable expiry
- Session idle timeout + max lifetime

Default attestation: "none" (enterprise device allowlists can be added later).

## Installation

### Build from Source

```bash
# Clone the repository
git clone https://github.com/EddisonSo/sshttp.git
cd sshttp

# Install frontend dependencies
cd client && npm install && cd ..

# Build everything (frontend + server with embedded static files)
make build
```

### Install the Daemon

```bash
# Install the binary (builds and copies to /usr/local/bin)
make install

# Create data directory
sudo mkdir -p /var/lib/sshttp
sudo chown $USER:$USER /var/lib/sshttp
```

### Quick Restart (Development)

```bash
# Rebuild and restart the daemon
make restart
```

### Systemd Service Setup

1. Copy and edit the service file:

```bash
# Copy the service file
sudo cp sshttp.service /etc/systemd/system/

# Edit to match your setup
sudo nano /etc/systemd/system/sshttp.service
```

2. Update the service file with your settings:

```ini
[Unit]
Description=sshttp - Secure Shell over HTTP
After=network.target

[Service]
Type=simple
User=youruser
Group=youruser
WorkingDirectory=/var/lib/sshttp
ExecStart=/usr/local/bin/sshttpd
Restart=always
RestartSec=5

# Environment configuration
Environment=SSHTTP_ADDR=:4422
Environment=SSHTTP_DATA_DIR=/var/lib/sshttp
Environment=SSHTTP_RP_ID=sshttp.example.com
Environment=SSHTTP_RP_ORIGIN=https://sshttp.example.com
Environment=SSHTTP_JWT_SECRET=your-secure-random-secret-here

[Install]
WantedBy=multi-user.target
```

3. Enable and start the service:

```bash
# Reload systemd
sudo systemctl daemon-reload

# Enable on boot
sudo systemctl enable sshttp

# Start the service
sudo systemctl start sshttp

# Check status
sudo systemctl status sshttp
```

### Register Your First User

```bash
# Generate a registration link
sshttpd --register yourusername
```

This outputs a one-time URL. Open it in your browser to register your passkey.

### Viewing Logs

```bash
# Follow logs
sudo journalctl -u sshttp -f

# View recent logs
sudo journalctl -u sshttp -n 100
```

## Production Deployment

The server embeds the frontend static files directly, so no separate web server is needed.

1. Build and install:
```bash
make install
```

2. Configure environment variables (in systemd service or shell):
   - Set `SSHTTP_RP_ID` to your domain (e.g., `sshttp.example.com`)
   - Set `SSHTTP_RP_ORIGIN` to your full origin (e.g., `https://sshttp.example.com`)

3. Configure TLS (required for WebAuthn):
   - Set `SSHTTP_TLS_CERT` and `SSHTTP_TLS_KEY` to your certificate paths
   - Or use a reverse proxy that terminates TLS
