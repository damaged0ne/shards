---
sidebar_position: 3
---

# Querying

Logs can be filtered by severity, message content, and attributes.

<img alt="shards Log Filtering" src="/img/docs/logs/query.png" class="card w-1200"/>

shards supports the `=` (equal), `~` (regex match), `!=` (not equal), and `!~` (regex not match) operators for attributes, 
and the `🔍` (contains) operator for message body.
Under the hood, the `contains` operator uses token-based search with ClickHouse full-text indexes for improved performance.

All positive filters (`=`, `~`) with the same attribute name are combined using `OR`, while negative filters (`!=`, `!~`) are combined using `AND`.

To make filtering easier, shards provides suggestions for attribute names and values.

<img alt="shards Log Filtering" src="/img/docs/logs/filter-suggest.png" class="card w-1200"/>

Filters can also be added from the log message details by clicking the `+` (add to search) or `–` (exclude from search) buttons.

<img alt="shards Log Filtering" src="/img/docs/logs/filter-from-details.png" class="card w-1200"/>
