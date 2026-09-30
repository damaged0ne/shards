---
sidebar_position: 4
---

# Slack

## Configure Slack

If you want to receive alerts in Slack, you’ll need to create a Slack App and make it available to shards.

In shards, open **Settings** → **Notifications**, then click **Configure** next to **Slack**.

Click **Create Slack app**. shards will open a new browser tab and send you over to the Slack website to create the Slack app. Select your Slack workspace.

When you click on Create Slack app, shards will pass along the app manifest, which Slack will use to set up your app.

:::info
You may get a warning that says: **This app is created from a 3rd party manifest**. 
This warning is expected (shards is the third party here). You can click **Configure** to see the app manifest shards sent along in the URL.
The manifest configures the required app settings and helps speed things along.
:::

On the Slack site for your newly created app, in the **Settings** > **Basic Information** tab, under **Install your app**, click on **Install to workspace**.

<img alt="Creating a Slack app" src="/img/docs/slack-integration-step1.png" class="card w-800"/>

On the next screen, click **Allow** to give shards access to your Slack workspace.

On the same page you can customize the app icon (for example, with the shards icon from `front/public/brand/icon.svg` in the shards repository, converted to PNG)

<img alt="Customize Slack App" src="/img/docs/slack-integration-step2.png" class="card w-600"/>

Then go to **OAuth and Permissions** and copy the **Bot User OAuth Token**.

<img alt="Slack Bot Token" src="/img/docs/slack-integration-step3.png" class="card w-800"/>

## Configure shards

* Go to **Settings** → **Notifications**
* Click **Configure** next to **Slack**
* Paste the token to the form
  <img alt="shards Slack Integration" src="/img/docs/slack-integration.png" class="card w-800"/>
* shards can send alerts into any public channel in your Slack workspace.
  Specify the channel name in the **Default Slack channel name** field.
  This channel will be used unless [overridden](/configuration/application-categories#notification-routing) by an application category's settings.
* You can also send a test alert to check the integration
  <img alt="shards Slack Test Alert" src="/img/docs/slack-integration-test.png" class="card w-800"/>
