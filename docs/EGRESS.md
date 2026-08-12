# grok2api egress

Uses a selectable residential chain with 1024Proxy sticky as the default:

- Route: local host -> US Los Angeles Hysteria2 -> 1024Proxy sticky residential exit
- Node DNS / URL: `<internal-sticky-proxy-endpoint>`
- Sticky lifetime: 120 minutes (`t-120`)
- Session policy: the first supplied session is fixed as active; additional sessions remain
  available on host-local listeners as manual standbys. There is no
  load balancing or automatic rotation between them.
- Static residential standby: Los Angeles static proxy on a host-local listener, routed
  through the same Los Angeles Hysteria2 hop.
- Dedicated sticky-session listeners map one-to-one to sticky sessions. Inside
  the application Docker network they are available through private service
  endpoints. These fixed paths
  are used by separate `grok_build` egress nodes so one failed residential
  session does not take every account offline.
- Legacy shared-path switch: `grok-egress dynamic1` through
  `grok-egress dynamic5`, `grok-egress static`, or `grok-egress status` controls
  only the selectable shared path. It does not change the
  five fixed sharded paths. `grok-egress dynamic` is an alias for session 1.
- Scopes: `grok_build`, `grok_web`, `grok_console`, `grok_web_asset`
- Network: the application container joins a private Docker network shared by
  the configured egress services

Previous sticky and shared Mihomo paths remain available for rollback. Neither is
modified by the 1024Proxy stack.

## grok_build sharding

The residential `grok_build` layout defines eight sticky egress nodes. Nodes can
be drained and disabled independently without changing the other paths:

| Role | Proxy URL |
| --- | --- |
| Sticky session shards | `<internal-session-proxy-endpoints>` |

Accounts are manually assigned round-robin across these nodes. The old shared
Build node is retained disabled with no assigned accounts for rollback. Do not
delete it.

## WgetCloud rotation pools

Eight low-cost proxy-pool nodes share nine independent WgetCloud exits: three
Singapore, three Japan, and three United States. Each pool has its own Mihomo
`load-balance` group with a rotated starting order and selects the next exit for
each new TCP connection.

| Role | Proxy URL |
| --- | --- |
| Managed rotation pools | `<internal-managed-proxy-endpoints>` |

These nodes must keep `proxyPool=true` in grok2api. The six earlier fixed
Older direct/via-relay nodes remain defined
but disabled and unassigned for rollback. Hong Kong and Taiwan subscription
nodes are intentionally excluded from the default pool.

Regenerate the WgetCloud config after a subscription refresh:

```sh
./update-wgetcloud-provider.sh
docker compose up -d --force-recreate \
  <managed-proxy-service> <proxy-bridge-service> <proxy-listener-service>
```

If the subscription endpoint cannot be reached directly, set
`WGETCLOUD_FETCH_PROXY` to a local HTTP proxy for the refresh command. The
subscription URL and generated provider files are private and must remain mode
`0600`.

Regenerate the 1024Proxy config after updating `1024proxy-sticky.list`:

```sh
./generate-1024proxy-config.rb
docker compose up -d --force-recreate <sticky-proxy-service>
```

For listener-only changes, validate the generated file and use Mihomo's
controller reload API. Recreating the sticky-proxy service terminates requests
currently using this chain, so only do it during a maintenance window.

## Kookeey US residential (trial)

Added 2026-08-05 without removing 1024 sticky nodes.

- Credentials: `kookeey-us-sticky.list` (11 sticky sessions)
- Mihomo: a dedicated provider configuration via its egress service (regional
  relay dialer -> provider exit)
- Host-local mixed listeners: read from the live Mihomo configuration
- Docker network: private service endpoints, omitted from documentation
- Egress nodes: read names and IDs from the Admin API/bootstrap at runtime
- Quality Guard watches the bootstrap node set; rotation remains sticky-only

Register panel uses the same host-local listener group. Exact bind addresses and
ports are intentionally omitted; read the live Compose/provider configuration.
