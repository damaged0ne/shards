---
sidebar_position: 1
---

# Synthetic probes

shards can check your endpoints from the outside, like Uptime Kuma or the Prometheus Blackbox Exporter, without
running another service. Probes are executed by the shards server itself, so they also work on a single Docker or
Docker Compose host.

| Type   | Target                              | Succeeds when                                                                       |
|--------|-------------------------------------|-------------------------------------------------------------------------------------|
| `http` | `http://` or `https://` URL         | the status code is in the expected range and the body contains the expected string |
| `tcp`  | `host:port`                         | a TCP connection can be established                                                 |
| `tls`  | `host:port`                         | the TLS handshake succeeds and the certificate is valid                             |
| `dns`  | domain name                         | the resolver returns at least one record of the requested type                      |

## Creating a probe

Go to **Uptime** in the **Infrastructure** section of the left menu and click **Add probe**. Only users with the
**Admin** or **Editor** role can create, change and delete probes.

| Setting                       | Default   | Description                                                                                                  |
|-------------------------------|-----------|--------------------------------------------------------------------------------------------------------------|
| Name                          |           | Unique within the project: letters, digits, `.`, `_` and `-`.                                                |
| Interval                      | `60s`     | How often the probe runs (10s..1h). The first run is spread randomly over the interval (jitter).             |
| Timeout                       | `10s`     | The maximum duration of a run, at most the interval.                                                         |
| Method                        | `GET`     | HTTP only.                                                                                                   |
| Expected status               | `200-399` | HTTP only: a list of codes and ranges, e.g. `200,204,301-302`.                                               |
| Response body must contain    |           | HTTP only: a substring that must be present in the first 1MB of the body.                                     |
| Request headers               |           | HTTP only. A `Host` header overrides the virtual host.                                                       |
| Follow redirects              | off       | HTTP only. When off, the redirect response itself is checked (3xx is within the default range).              |
| Skip TLS verification         | off       | HTTP and TLS: the certificate isn't validated, but its expiry is still reported.                             |
| Record type / DNS server      | `A`       | DNS only: `A`, `AAAA`, `CNAME`, `MX`, `TXT` or `NS`; the server's resolver is used unless a server is given. |
| Application                   |           | Links the probe to an application, see below.                                                                |
| Paused                        | off       | Paused probes keep their configuration but don't run.                                                        |

**Run test** executes the probe once from the server and shows the result (status, response time by phase,
certificate, error) without saving it.

The same operations are available through the API (`/api/project/<project>/probes`) and to AI agents through the
[MCP server](/mcp/overview): `list_probes`, `get_probe_results`, `create_probe`, `update_probe`, `delete_probe`.

## Applications and alerts

A probe linked to an application shows up in the **Uptime** report of the application: the probe status, the uptime
over the selected time range, availability and response time charts, the response time broken down by phase, and the
certificate expiration. The probe checks below fire alerts for this application, so they are routed to the
notification channels of its [category](/configuration/application-categories).

A probe that isn't linked to an application gets an application of its own (kind `Probe`, namespace `probes`) that
only has the Uptime report.

| Check                                  | Default threshold     | Built-in alerting rule                                    |
|----------------------------------------|-----------------------|-----------------------------------------------------------|
| Probe availability                     | 2 failed runs in a row | **Probe is failing**, critical                            |
| Probe latency                          | 2s                    | **Slow probe response**, warning (fires after 2 minutes) |
| TLS certificate expiration             | 14 days               | **TLS certificate expires soon**, warning                 |
| TLS certificate expiration (critical)  | 3 days                | **TLS certificate is about to expire**, critical          |
| TLS certificate validity               |                       | **Invalid TLS certificate**, critical                     |

The thresholds can be adjusted per application (or project-wide) on the **Inspections** tab of the project settings or
by clicking the check in the Uptime report. A certificate is invalid when it is expired, isn't trusted by the system
root CAs, or doesn't match the hostname; probes with **Skip TLS verification** never report invalid certificates.

## Metrics

The results are written into the project's metrics storage (Prometheus via remote write, or ClickHouse), exactly like
the metrics of the agents, so they can be used in dashboards and PromQL alerting rules:

| Metric                                                    | Description                                                                         |
|-----------------------------------------------------------|-------------------------------------------------------------------------------------|
| `shards_probe_up`                                         | 1 if the run succeeded, 0 otherwise                                                 |
| `shards_probe_duration_seconds{phase}`                    | `dns`, `connect`, `tls`, `ttfb` (from the request being sent to the first byte) and `total` |
| `shards_probe_http_status_code`                           | the status code of the (last) HTTP response                                         |
| `shards_probe_consecutive_failures`                       | the number of failed runs in a row                                                  |
| `shards_probe_tls_cert_expiry_seconds`                    | the Unix time the certificate expires at (the earliest expiry in the presented chain) |
| `shards_probe_tls_cert_info{issuer,subject,not_after}`    | always 1                                                                            |
| `shards_probe_tls_cert_valid`                             | 1 if the certificate is valid, only when the verification is enabled               |

Every series has the `probe_id`, `probe_name`, `probe_type` and `target` labels. Days until a certificate expires:
`(shards_probe_tls_cert_expiry_seconds - time()) / 86400`.

The latest result of each probe (including the error message, which can't be stored as a metric) is also kept in the
shards database and shown on the Uptime page even if the project has no metrics storage configured.

## Security

Probes connect from the shards server, which may have access to networks your users don't. To prevent server-side
request forgery, probes never connect to link-local and cloud metadata addresses: `169.254.0.0/16` (including the
`169.254.169.254` metadata endpoint of AWS, GCP, Azure and OCI), `fe80::/10` and `fd00:ec2::254`. Every resolved
address is checked, the checked address is the one connected to (no DNS rebinding), redirects and custom DNS servers
are subject to the same rules, and proxy environment variables are ignored.

To allow some of these networks, use `--probes-allowed-networks` (`PROBES_ALLOWED_NETWORKS`), e.g.
`--probes-allowed-networks=169.254.10.0/24`. Private and loopback addresses are allowed: probing services on the same
host is a primary use case.

## Scheduling

The probes run on the primary shards instance (in an HA setup with Postgres, on the one holding the primary lock),
at most `--probes-concurrency` (default 16) at the same time. Changes made in the UI or via the API take effect
within a couple of seconds; probes of deleted projects are dropped. To disable probes entirely, use
`--disable-probes`.
