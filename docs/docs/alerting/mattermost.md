---
sidebar_position: 11
---

# Mattermost

shards posts incident and alert notifications to Mattermost through an incoming webhook, using Slack-compatible message
attachments colored by severity.

## Create an incoming webhook

* Go to **Integrations** → **Incoming Webhooks** → **Add Incoming Webhook**
* Choose the default channel. Leave **Lock to this channel** disabled if shards should be able to post to another channel.
* Copy the webhook URL

## Configure shards

* Go to the **Project Settings** → **Integrations**
* Create a Mattermost integration and paste the webhook URL
* Optionally, override the channel and the username (the webhook must allow it)
* Choose what to notify of: incidents, alerts, and comments that people and agents post on incidents and alerts
* Click **Send test alert** to check the integration

The attachment title links back to the incident or alert (built from the base URL on the Integrations page). Which
application categories are sent to Mattermost is configured in the [application categories](/configuration/application-categories).

The integration can also be defined in the [configuration file](/configuration/configuration)
(`notificationIntegrations.mattermost`).
