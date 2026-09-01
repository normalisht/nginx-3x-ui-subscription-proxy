# nginx-3x-ui-subscription-proxy

.py

## Overview
This project allows you to set up an Nginx-based reverse proxy that fetches and aggregates subscription configurations from multiple 3x-UI servers. It simplifies subscription management by unifying configurations in a single endpoint.

The proxy uses a multi-service architecture:
- **Nginx**: Listens on ports 80/443, handles SSL termination, and proxies subscription requests to the Python backend
- **Python**: Runs as a standalone HTTP server (port 8080) that fetches and aggregates subscription configurations from multiple 3x-UI servers

## Important Notes

1. Each client must have the same **subscription ID** across all your servers.
2. Subscription encryption must be enabled on all 3x-UI servers.


## Setup Instructions

### 1. Clone the Repository
```bash
git clone https://github.com/apa4h/nginx-3x-ui-subscription-proxy.git
cd nginx-3x-ui-subscription-proxy
```

### 2. Copy the Environment File
```bash
cp .env.template .env
```

### 3. Configure Environment Variables
Edit the `.env` file and fill in the following variables with your own data:

| Variable        | Description                                                                                     |
|-----------------|-------------------------------------------------------------------------------------------------|
| `SITE_HOST`     | Domain name for your Nginx server (e.g., `subserver.example`).                                           |
| `SERVERS`       | List of 3x-UI server URLs to aggregate subscriptions from (e.g., `https://server1.com/sub/ https://server2.com/sub/`). |
| `SUB`           | Static part of the subscription path (e.g., `sub`).                                             |
| `PORT`          | Port for the Python server to listen on (default: 8080).                                             |
| `EXTERNAL_SUBSCRIPTIONS` | Optional. List of full third-party subscription URLs (e.g., other VPN services) that already include the ID and return base64 directly — unlike `SERVERS`, nothing is appended to these (e.g., `https://vpn-service.com/sub/abc123 https://another.com/sub/xyz789`). |

#### Subscription URL Format

Once you've configured the environment variables, your subscription URL will look like this:
`https://subserver.example/sub/subscription_ID`

Where:
- `subserver.example` is the domain you set in the `SITE_HOST` variable.
- `sub` is the static part of the subscription path, set in the `SUB` variable.
- `subscription_ID` is the unique ID for each client from 3x-ui.

### 4. Start the Application
Run the following command to start the application:
```bash
docker compose up -d
```

This will build and start both the Nginx and Python containers with the provided configuration.

## How It Works
- Nginx listens on ports 80 and 443 (SSL)
- Subscription requests to `/sub/<subscription_ID>` are proxied to the Python service on port 8080
- The Python service fetches configurations from all servers listed in `SERVERS` (appending `subscription_ID` to each URL) and from third-party links listed in `EXTERNAL_SUBSCRIPTIONS` (fetched as-is)
- Configurations are decoded (base64), combined, re-encoded, and returned to the client
- SSL certificates are managed automatically by Certbot

## Architecture
```
┌─────────────────┐     HTTPS /sub/*     ┌──────────────────┐
│                 │ ──────────────────────▶ │                  │
│     Nginx       │   HTTP proxy_pass      │     Python       │
│  (ports 80/443) │ ◀───────────────────── │  (port 8080)     │
│                 │  HTTP to backend       │                  │
└─────────────────┘                         └──────────────────┘
      │                                                            │
      │ SSL termination                                            │
      │ Certbot integration                                        │
      │                                                            │
      └────────────────────────────────────────────────────────────┘
                              DNS: python
```

## Example Configuration
Here is an example `.env` file:
```dotenv
SITE_HOST=example.com
SERVERS="https://server1.com/sub/ https://server2.com/sub/"
SUB=sub
PORT=8080
EXTERNAL_SUBSCRIPTIONS="https://vpn-service.com/sub/abc123"
```

## License
This project is licensed under the MIT License. See the `LICENSE` file for details.

## Contributing
Contributions are welcome! Feel free to open an issue or submit a pull request.
