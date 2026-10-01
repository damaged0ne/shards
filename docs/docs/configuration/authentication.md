---
sidebar_position: 2
---

# Authentication

After installation, shards will prompt you to set a password for the admin user:

<img alt="Setting Admin Password" src="/img/docs/admin_password.png" class="card w-1200"/>

To prevent someone else from setting the admin password before you, you can specify the initial password using the 
`--auth-bootstrap-admin-password` CLI argument or the `AUTH_BOOTSTRAP_ADMIN_PASSWORD` environment variable. 
This initial password can be changed later through the UI.

## Anonymous mode

To disable authentication, use the `--auth-anonymous-role` CLI argument or the `AUTH_ANONYMOUS_ROLE` environment variable, 
setting it to one of the following roles: `Admin`, `Editor`, or `Viewer`.

## Reset admin password

To reset admin password, use the following command:

```bash
$ shards set-admin-password
Enter new password:
Confirm new password:
Admin password set successfully.
```

## Manage users

To manage shards users, go to the **Project Settings**, click on **Organization**:

<img alt="Manage Users" src="/img/docs/users.png" class="card w-1200"/>

To add a new user, click "Add user", fill out the form, and select a role.

<img alt="Add user" src="/img/docs/add_user.png"  class="card w-600"/>

shards includes three predefined roles: `Admin`, `Editor`, and `Viewer`.

## Service accounts and API keys

A service account is a user meant for programmatic access: it has no password and cannot log in, and it authenticates with an API key instead.
Use it for anything that talks to shards without a person behind it, such as an autonomous AI agent using the [MCP endpoint](/mcp/overview#authentication) or a script calling the HTTP API.

A service account has a regular role, so RBAC applies to it exactly as to a human user: a `Viewer` can read everything but cannot resolve alerts,
and an `Editor` can.

To create a service account, click **Add user**, check **Service account**, enter a name, and select a role.

<img alt="Add service account" src="/img/docs/add_service_account.png" class="card w-600"/>

Service accounts are listed next to regular users. The lock icon marks accounts defined in the config file (see below).

<img alt="Users and service accounts" src="/img/docs/users_service_accounts.png" class="card w-1200"/>

Every user can manage their own API keys through **API keys** in the user menu (bottom left).

<img alt="API keys in the user menu" src="/img/docs/user_menu_api_keys.png" class="card w-400"/>

Users with the permission to manage users (`Admin`) can also manage the keys of any user, including service accounts, by clicking the key icon next to the user.
A newly created key is shown only once, so copy it right away. shards stores only its hash.

<img alt="API keys of a service account" src="/img/docs/service_account_api_keys.png" class="card w-600"/>

A user can have more than one key, which allows rotating a key without downtime: create a new key, switch the client to it, and then delete the old one.

The client sends the key as a bearer token, and it works for both the MCP endpoint and the HTTP API:

```
Authorization: Bearer <api key>
```

### Defining service accounts in the config file

For infrastructure-as-code setups, service accounts and their keys can be defined in the [config file](/configuration/configuration).
shards creates such accounts on startup and keeps their keys in sync with the config: keys listed here are added, keys no longer listed are removed, and unchanged keys are left untouched,
so a config change never interrupts an agent whose key is unchanged. Descriptions are required and must be unique within an account. Accounts defined this way are locked in the UI, and their keys are managed only through the config.

```yaml
auth:
  serviceAccounts:
    - name: claude-agent
      role: Viewer
      apiKeys:
        - key: ${CLAUDE_AGENT_API_KEY} # environment variables are expanded
          description: production investigation agent
```

To rotate a config-defined key without downtime, list both the old and the new key (under different descriptions) during the rollout, switch the agents to the new key, then remove the old one.

On Kubernetes, the [Coroot operator](/installation/k8s-operator) exposes the same settings as `spec.serviceAccounts` of the `Coroot` resource, and a key can reference a Secret through `keySecret`.
The operator generates the Secret with a random key if it does not exist, so the agent's Deployment can mount the same Secret and no key ever needs to be copied by hand:

```yaml
spec:
  serviceAccounts:
    - name: claude-agent
      role: Viewer
      apiKeys:
        - description: production investigation agent
          keySecret:
            name: shards-claude-agent
            key: api-key
```
