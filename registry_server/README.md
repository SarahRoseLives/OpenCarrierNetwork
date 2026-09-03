# OCN Registry Server

The registry is the federation hub: it assigns area codes (200-999, reserving
800/900 for system use), answers routing queries, relays inter-server calls
between OCN servers, pushes FCM wake-ups using the network's shared Firebase
project, and serves the opencarrier.network "coming soon" website.

## Local run (dev, plaintext)

```sh
cd registry_server
go build -o /tmp/regdev .
/tmp/regdev -plaintext -http-addr 127.0.0.1:8099 -grpc-addr 127.0.0.1:7443 -db /tmp/reg.db
```

## Production run (Let's Encrypt + gRPC TLS + FCM)

```sh
cd registry_server
go build -o registry-server .
./registry-server \
  -domain opencarrier.network \
  -email sarahroselives@protonmail.com \
  -http-addr :80 -https-addr :443 -grpc-addr :7443 \
  -db /var/lib/ocn-registry/registry.db \
  -cache-dir /var/lib/ocn-registry/certs \
  -fcm-creds /etc/ocn-registry/firebase-service-account.json
```

- Port 80 is used for ACME (Let's Encrypt) and redirects; 443 serves the site;
  :7443 is the TLS gRPC endpoint OCN servers talk to.
- The Let's Encrypt certificate is cached in `-cache-dir` and is reused by
  other processes on the host if they read the cached PEMs there.
- `-fcm-creds` points at the **official shared Firebase** service account so
  `PushDevice` can ring federated phones. Without it `PushDevice` is disabled.

## Deploying as a systemd service

See `deploy/ocn-registry.service`. On a Debian/Ubuntu host:

```sh
sudo useradd -r -s /usr/sbin/nologin ocn
sudo mkdir -p /opt/ocn-registry /var/lib/ocn-registry /etc/ocn-registry
sudo install -m0755 registry-server /opt/ocn-registry/
sudo install -m0600 /path/to/firebase-service-account.json /etc/ocn-registry/firebase-service-account.json
sudo chown -R ocn:ocn /opt/ocn-registry /var/lib/ocn-registry
sudo install -m0644 deploy/ocn-registry.service /etc/systemd/system/
sudo systemctl daemon-reload && sudo systemctl enable --now ocn-registry
```

Open firewall ports 22, 80, 443, 7443 (and any ocnserver WS/admin ports).

## Running OCN servers against it (federation)

Add to an ocnserver's config or flags:

```
registry_address          registry.opencarrier.network:7443   # or opencarrier.network:7443
registry_insecure         false
registry_area_code        "212"          # your requested area ("" = auto)
federation_addr           ":9110"        # inter-server gRPC listen
federation_public_address "opencarrier.network:9110"  # reachable address advertised to registry
federation_insecure       false
```

The server keeps the area code it was assigned across restarts (stored in its
SQLite `settings`). 800 and 900 cannot be requested.

## Cross-server call test (local)

`ocnserver/cmd/wsprobe` is a small WS test client. With a registry and two
federated ocnservers running locally (see the smoke configs this repo used):

```sh
# answer side (callee on Beta)
wsprobe -ws ws://127.0.0.1:9200/ws -key beta/server.key -mode answer
# caller side (dial full 10-digit number on Alpha)
wsprobe -ws ws://127.0.0.1:9101/ws -key alpha/server.key -mode call -to 3107654321
```

The caller should see `call_ringing`, then `call_connected` with the callee's
answer, and trickled ICE in both directions.
