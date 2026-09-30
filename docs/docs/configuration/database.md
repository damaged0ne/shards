---
sidebar_position: 7
---

# Database

shards requires a database to store its configuration, such as projects and Prometheus connection details.

## SQLite (default)

By default, shards uses an embedded sqlite database. For production installations, we recommend users to use a 
robust database, such as Postgres. This allows you to run several shards replicas for high availability and backup the database.

## Postgres

Create role and database:

```sql
CREATE ROLE shards WITH LOGIN PASSWORD 'password';
CREATE DATABASE shards WITH OWNER = shards;
```

You can configure shards to use Postgres by setting the `--pg-connection-string` command line argument or the `PG_CONNECTION_STRING` environment variable:

```bash
docker run -d --name shards \
  -p 8080:8080 \
  -e PG_CONNECTION_STRING="postgres://shards:password@127.0.0.1:5432/shards?sslmode=disable" \
  ghcr.io/damaged0ne/shards
``` 

Here is an example of how to format the `PG_CONNECTION_STRING` variable using a Kubernetes secret:

```yaml
...
env:
- name: PGPASSWORD
  valueFrom: { secretKeyRef: { name: shards.pg.credentials, key: password } }
- name: PG_CONNECTION_STRING
  value: "host=shards-db user=shards password=$(PGPASSWORD) dbname=shards sslmode=require connect_timeout=1"
  ...
```
  
To learn more about the connection string format follow the [Postgres documentation](https://www.postgresql.org/docs/current/libpq-connect.html#LIBPQ-CONNSTRING).

### Connection Poolers

shards is incompatible with PostgreSQL connection poolers like pgbouncer when they're configured to run in transactional mode. This is because shards relies on connection-level prepared statements, which don't persist across transactions in pooled connections. If you need to use a connection pooler, configure it to operate in session mode to ensure prepared statements remain available throughout the connection lifecycle.

