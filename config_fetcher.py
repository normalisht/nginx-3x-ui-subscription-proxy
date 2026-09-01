#!/usr/bin/env python3
"""Python equivalent of config_fetcher.lua - fetches and aggregates 3x-UI subscriptions."""

import base64
import os
import re
import sys
import time
from http.server import HTTPServer, BaseHTTPRequestHandler

import requests

from dotenv import load_dotenv

load_dotenv()

def get_servers():
    servers_str = os.environ.get("SERVERS", "")
    return servers_str.split()


def get_external_subscriptions():
    external_str = os.environ.get("EXTERNAL_SUBSCRIPTIONS", "")
    return external_str.split()


def fetch_and_decode(url):
    try:
        response = requests.get(url, timeout=10, verify=False)
    except Exception as e:
        print(f"Error: Request failed for {url}: {e}", file=sys.stderr)
        return None

    if response.status_code != 200:
        print(f"Error: Failed to fetch from {url}, status: {response.status_code}", file=sys.stderr)
        return None

    text = response.text.strip()
    padded = text + "=" * (-len(text) % 4)
    for decoder in (base64.b64decode, base64.urlsafe_b64decode):
        try:
            return decoder(padded).decode("utf-8")
        except Exception:
            continue

    print(f"Error: Failed to decode base64 from {url}", file=sys.stderr)
    return None


def fetch_configs(servers, sub_id, external_subscriptions=None):
    configs = []
    if os.environ.get("ROUTING_ENABLED") == "1":
        site_host = os.environ.get("SITE_HOST")
        configs.extend([
            f'://autorouting/onadd/https://{site_host}/routing/routing.json\n',
            f'://routing/onadd/https://{site_host}/routing/routing.json\n',
        ])

    for base_url in servers:
        url = base_url.rstrip("/") + "/" + sub_id
        decoded_config = fetch_and_decode(url)
        if decoded_config is not None:
            configs.append(decoded_config)

    for url in external_subscriptions or []:
        decoded_config = fetch_and_decode(url)
        if decoded_config is not None:
            configs.append(decoded_config)

    return configs


def combine_and_encode(configs):
    if not configs:
        return None
    combined = "".join(configs)
    encoded = base64.b64encode(combined.encode("utf-8")).decode("utf-8")
    return encoded


class ConfigHandler(BaseHTTPRequestHandler):
    def log_message(self, format, *args):
        pass

    def do_GET(self):
        sub_var = os.environ.get("SUB", "sub")
        pattern = f"^/{sub_var}/([^/]+)/?$"
        match = re.match(pattern, self.path)
        if not match:
            self.send_response(404)
            self.send_header("Content-Type", "text/plain")
            self.end_headers()
            self.wfile.write(b"Not Found")
            return
        sub_id = match.group(1)
        servers = get_servers()
        external_subscriptions = get_external_subscriptions()
        configs = fetch_configs(servers, sub_id, external_subscriptions)
        if configs:
            encoded_combined = combine_and_encode(configs)
            self.send_response(200)
            self.send_header("Content-Type", "text/plain; charset=utf-8")
            self.end_headers()
            self.wfile.write(encoded_combined.encode("utf-8"))
        else:
            self.send_response(502)
            self.send_header("Content-Type", "text/plain")
            self.end_headers()
            self.wfile.write(b"No configs available")


def main():
    port = int(os.environ.get("PORT", 8080))
    server = HTTPServer(("", port), ConfigHandler)
    print(f"Starting server on port {port}...", file=sys.stderr)
    while True:
        try:
            server.serve_forever()
        except KeyboardInterrupt:
            print("\nShutting down...", file=sys.stderr)
            server.shutdown()
        time.sleep(3)


if __name__ == "__main__":
    main()
