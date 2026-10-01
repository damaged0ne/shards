---
sidebar_position: 10
---

# Discord

shards posts incident and alert notifications to a Discord channel through a webhook, as embeds colored by severity
(red for critical, yellow for warning, green when resolved). Mentions in alert texts are never resolved, so a
notification can't ping `@everyone`.

## Create a webhook

* Open **Server Settings** → **Integrations** → **Webhooks** (or **Edit Channel** → **Integrations** of a channel)
* Click **New Webhook**, choose the channel and, optionally, rename it (e.g. *shards*)
* Click **Copy Webhook URL**

## Configure shards

* Go to the **Project Settings** → **Integrations**
* Create a Discord integration and paste the webhook URL
* Choose what to notify of: incidents, alerts, and comments that people and agents post on incidents and alerts
* Click **Send test alert** to check the integration

Each embed links back to the incident or alert (built from the base URL on the Integrations page). Which application
categories are sent to Discord is configured in the [application categories](/configuration/application-categories).

The integration can also be defined in the [configuration file](/configuration/configuration)
(`notificationIntegrations.discord`).
