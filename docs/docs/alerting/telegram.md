---
sidebar_position: 9
---

# Telegram

shards sends incident and alert notifications to a Telegram group, supergroup (optionally to a forum topic) or
channel through a bot.

## Create a bot

* Open a chat with [@BotFather](https://t.me/BotFather), send `/newbot` and follow the instructions
* Copy the bot token (`123456789:AA...`)
* Add the bot to the group or channel. In channels, the bot must be an administrator allowed to post messages.

## Find the chat id

* Post a message to the chat, then open `https://api.telegram.org/bot<token>/getUpdates` and look for `"chat":{"id":...}`.
  Supergroup and channel ids start with `-100`.
* Public channels can also be addressed as `@channelusername`.
* To post into a forum topic, also copy the topic id (`message_thread_id` in `getUpdates`, or the last number in a
  message link `https://t.me/c/<chat>/<topic>/<message>`).

## Configure shards

* Go to the **Project Settings** → **Integrations**
* Create a Telegram integration and fill in the bot token, the chat id and, optionally, the topic
* Choose what to notify of: incidents, alerts, and comments that people and agents post on incidents and alerts
* Click **Send test alert** to check the integration

Messages are sent with the HTML parse mode: the title with the severity, the details, and a link back to the incident
or alert (built from the base URL on the Integrations page). Which application categories are sent to Telegram is
configured in the [application categories](/configuration/application-categories).

The integration can also be defined in the [configuration file](/configuration/configuration)
(`notificationIntegrations.telegram`).
