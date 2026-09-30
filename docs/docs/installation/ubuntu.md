---
sidebar_position: 8
---

# Ubuntu & Debian


**Step #1: Installing ClickHouse**

```bash
sudo apt install -y apt-transport-https ca-certificates curl gnupg
curl -fsSL 'https://packages.clickhouse.com/rpm/lts/repodata/repomd.xml.key' | sudo gpg --dearmor -o /usr/share/keyrings/clickhouse-keyring.gpg
echo "deb [signed-by=/usr/share/keyrings/clickhouse-keyring.gpg] https://packages.clickhouse.com/deb stable main" | sudo tee /etc/apt/sources.list.d/clickhouse.list
sudo apt update
sudo DEBIAN_FRONTEND=noninteractive apt install -y clickhouse-server clickhouse-client
sudo service clickhouse-server start
```

**Step #2: Installing Prometheus**

shards requires Prometheus with support for Remote Write Receiver, which has been available since v2.25.0.

```bash
sudo apt install -y prometheus
sudo service prometheus start
```

Enable Remote Write Receiver in Prometheus by adding the `--enable-feature=remote-write-receiver` argument to the `/etc/default/prometheus` file:

```bash
# Set the command-line arguments to pass to the server.
# Due to shell escaping, to pass backslashes for regexes, you need to double
# them (\\d for \d). If running under systemd, you need to double them again
# (\\\\d to mean \d), and escape newlines too.
ARGS="--enable-feature=remote-write-receiver"
```

Restart Prometheus:

```bash
sudo service prometheus restart
```

**Step #3: Installing shards**

```bash
curl -sfL https://raw.githubusercontent.com/damaged0ne/shards/main/deploy/install.sh | \
  BOOTSTRAP_PROMETHEUS_URL="http://127.0.0.1:9090" \
  BOOTSTRAP_REFRESH_INTERVAL=15s \
  BOOTSTRAP_CLICKHOUSE_ADDRESS=127.0.0.1:9000 \
  sh -
```

**Step #4: Installing shards-node-agent**

```bash
curl -sfL https://raw.githubusercontent.com/damaged0ne/shards-node-agent/main/install.sh | \
  COLLECTOR_ENDPOINT=http://127.0.0.1:8080 \
  SCRAPE_INTERVAL=15s \
  sh -
```

**Step #5: Accessing shards**

Access shards at: http://NODE_IP:8080.

**Upgrade shards**

To upgrade shards, re-run the installation command from Step #3 — the install script downloads the latest version and restarts the service.
To upgrade shards-node-agent, re-run the installation command from Step #4.

**Uninstall shards**

To uninstall shards run the following command:

```bash
/usr/local/bin/shards-uninstall.sh
```
