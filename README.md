# ChameleonNet

[![Go Version](https://img.shields.io/github/go-mod/go-version/Sammmmmmmssssssss/chameleonnet)](https://golang.org/dl/)
[![Build Status](https://github.com/Sammmmmmmssssssss/chameleonnet/actions/workflows/go.yml/badge.svg)](https://github.com/Sammmmmmmssssssss/chameleonnet/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/Sammmmmmmssssssss/chameleonnet)](https://goreportcard.com/report/github.com/Sammmmmmmssssssss/chameleonnet)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](CONTRIBUTING.md)

**ChameleonNet** is a zero-dependency Go traffic morphing tunnel that defeats Deep Packet Inspection (DPI) by making encrypted traffic statistically indistinguishable from benign protocols. It combines a SOCKS5/HTTP CONNECT proxy with packet padding, chaff injection, and Poisson-distributed traffic shaping.

```
Client App ──SOCKS5──▶ Local Proxy ──morphed tunnel──▶ Remote Relay ──▶ Internet
                         │                                │
                         ├─ Chaff Injection               ├─ Chaff Filtering
                         ├─ Packet Padding                ├─ Depadding
                         ├─ Traffic Shaping               └─ Decryption
                         └─ AES-256-GCM Encryption
```

## Features

- **Dual protocol listener** — SOCKS5 (RFC 1928) + HTTP CONNECT on a single port via `br.Peek(1)` multiplexing
- **DPI evasion** — configurable traffic morphing profiles with chaff injection, packet padding, and Poisson-distributed delays
- **AES-256-GCM encryption** — direction-specific sub-keys derived via SHA-256 HMAC KDF with per-packet random nonces
- **Zero-copy relay** — `sync.Pool`-backed buffer pools eliminate heap allocation on hot data paths
- **3 traffic profiles** — Spotify, YouTube, and Generic with distinct statistical fingerprints
- **Sub-15MB memory footprint** — GC-tuned for Apple Silicon / resource-constrained environments
- **Graceful shutdown** — context-based draining with configurable timeouts
- **60s metrics dump** — real-time visibility into traffic, connections, memory, GC, buffer pool, and errors

## Quick Start

### 1. Build

```bash
git clone https://github.com/Sammmmmmmssssssss/chameleonnet.git
cd chameleonnet
go build -o chameleon ./cmd/chameleon/
```

### 2. Run the Relay Server

```bash
CHAMELEON_MODE=server \
  CHAMELEON_LISTEN=0.0.0.0:10080 \
  CHAMELEON_PASSPHRASE="your-16-char-min-passphrase" \
  ./chameleon
```

### 3. Run the Client Proxy

```bash
CHAMELEON_MODE=client \
  CHAMELEON_LISTEN=127.0.0.1:1080 \
  CHAMELEON_TARGET=your-server-ip:10080 \
  CHAMELEON_PASSPHRASE="your-16-char-min-passphrase" \
  ./chameleon
```

### 4. Route traffic through it

```bash
# SOCKS5
curl -x socks5h://127.0.0.1:1080 https://ifconfig.me

# HTTP CONNECT
curl -x http://127.0.0.1:1080 https://ifconfig.me
```

## Configuration

All configuration is via environment variables. A JSON config file can also be specified with `-config`:

```bash
./chameleon -config /etc/chameleon/server.json
```

### Core

| Variable | Description | Default |
|---|---|---|
| `CHAMELEON_MODE` | `client` or `server` | — |
| `CHAMELEON_LISTEN` | Local listen address | `127.0.0.1:1080` |
| `CHAMELEON_TARGET` | Remote relay address (client mode) | — |
| `CHAMELEON_PASSPHRASE` | Shared secret (min 16 chars) | — |

### Traffic Morphing

| Variable | Description | Default |
|---|---|---|
| `CHAMELEON_PROFILE` | `spotify`, `youtube`, `generic` | `spotify` |
| `CHAMELEON_POISSON_LAMBDA` | Chaff injection rate (events/sec) | profile-dependent |
| `CHAMELEON_CHAFF_RATIO` | Chaff-to-real packet ratio | profile-dependent |
| `CHAMELEON_MIN_SHAPE_DELAY` | Min traffic shaping delay | `10ms` |
| `CHAMELEON_MAX_SHAPE_DELAY` | Max traffic shaping delay | `500ms` |

### Timeouts

| Variable | Description | Default |
|---|---|---|
| `CHAMELEON_HANDSHAKE_TIMEOUT` | Tunnel handshake timeout | `10s` |
| `CHAMELEON_READ_TIMEOUT` | Per-read timeout | `60s` |
| `CHAMELEON_WRITE_TIMEOUT` | Per-write timeout | `60s` |

### Resource Limits

| Variable | Description | Default |
|---|---|---|
| `CHAMELEON_MAX_CONNECTIONS` | Max concurrent connections | `100` |
| `CHAMELEON_BUFFER_SIZE` | Relay buffer size | `32768` |
| `CHAMELEON_KDF_ITERATIONS` | PBKDF2-like iteration count | `100000` |

## Architecture

```
┌──────────────┐     SOCKS5/     ┌─────────────────────┐     Encrypted      ┌────────────────┐
│  User App    │──── HTTP ──────▶│  Local Proxy        │────── Tunnel ─────▶│  Remote Relay  │──── Internet
│  (curl,      │     CONNECT     │  (chameleon client) │     + Morphing     │  (chameleon    │
│   browser)   │                 │                     │                    │   server)      │
└──────────────┘                 └─────────────────────┘                    └────────────────┘
                                        │                                            │
                                        │ ┌──────────────────────┐                  │ ┌──────────────┐
                                        │ │  Traffic Morpher     │                  │ │  Demorpher   │
                                        │ │  ├─ ChaffInjector    │                  │ │  ├─ Chaff    │
                                        │ │  ├─ Padder           │                  │ │  │  Filter   │
                                        │ │  ├─ Shaper (Poisson) │                  │ │  ├─ Depadder │
                                        │ │  └─ AES-256-GCM Enc  │                  │ │  └─ AES-256- │
                                        │ └──────────────────────┘                  │ │     GCM Dec  │
                                        │                                            │ └──────────────┘
                                        │ ┌──────────────────────┐                  │
                                        │ │  Buffer Pool         │                  │
                                        │ │  (sync.Pool, 4 size  │                  │
                                        │ │   classes)           │                  │
                                        │ └──────────────────────┘                  │
```

### Packet Flow

1. **Client connection** arrives at local proxy via SOCKS5 or HTTP CONNECT
2. **Protocol detection** uses `bufio.Reader.Peek(1)` — `0x05` = SOCKS5, ASCII letter = HTTP
3. **Traffic morphing pipeline** processes outgoing data:
   - `ChaffInjector` inserts decoy packets at Poisson-distributed intervals
   - `Padder` rounds packet sizes to nearest bucket boundary
   - `Shaper` applies Exp+LogNormal mixture delay distribution
   - `Encryptor` seals each packet with AES-256-GCM + random nonce
4. **Wire format**: `[4B length][12B nonce][ciphertext][16B GCM tag]`
5. **Remote relay** receives, filters chaff, decrypts, depads, and forwards to target
6. **Response** follows the reverse path through the same pipeline

## Development

### Prerequisites

- Go 1.22+
- macOS, Linux, or Windows

### Test

```bash
go test -v -race -count=1 ./...
```

### Lint

```bash
# Install golangci-lint, then:
golangci-lint run ./...
```

### Benchmark

```bash
go test -bench=. -benchmem ./internal/...
```

## Docker

```bash
# Build
docker build -t chameleonnet .

# Run server
docker run -e CHAMELEON_MODE=server \
  -e CHAMELEON_PASSPHRASE=your-passphrase \
  -p 10080:10080 chameleonnet

# Or use docker-compose (client + server in one network)
docker-compose up
```

## Security

- **Encryption**: AES-256-GCM with per-packet random 12-byte nonces
- **Key derivation**: SHA-256 HMAC with 100,000 iterations + random 32-byte salt
- **Direction isolation**: Client and server use domain-separated sub-keys (`salt XOR 0x01` / `salt XOR 0x02`)
- **No third-party dependencies**: full crypto stack is Go standard library only

See [SECURITY.md](SECURITY.md) for the full security policy and responsible disclosure.

## License

MIT
