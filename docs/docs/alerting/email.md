---
sidebar_position: 12
---

# Email

shards sends incident and alert notifications by email through any SMTP server. Each message has a plain text and
an HTML part with the details and a link back to the incident or alert.

## Configure shards

* Go to the **Project Settings** → **Integrations**
* Create an Email integration:

| Setting        | Description                                                                                                   |
|----------------|---------------------------------------------------------------------------------------------------------------|
| SMTP host/port | e.g. `smtp.example.com` and `587`                                                                             |
| Encryption     | **STARTTLS** (default, port 587; the server must support it), **TLS** (implicit TLS / SMTPS, port 465) or **None** |
| Username/password | Optional, sent with `AUTH PLAIN`. Credentials are never sent over an unencrypted connection to a remote host. |
| Skip TLS verification | Don't validate the server certificate (self-signed certificates)                                        |
| From           | The sender, e.g. `shards <alerts@example.com>`                                                                |
| To             | A comma-separated list of recipients                                                                         |

* Choose what to notify of: incidents, alerts, and comments that people and agents post on incidents and alerts
* Click **Send test alert** to check the integration

Which application categories are sent by email is configured in the [application categories](/configuration/application-categories).

The integration can also be defined in the [configuration file](/configuration/configuration)
(`notificationIntegrations.email`).
