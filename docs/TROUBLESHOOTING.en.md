# Troubleshooting

## Cloudflare Error 1033

### Start with the management page

Open <http://127.0.0.1:8788/admin/>, select **Connectivity diagnostics**, enter the public Server URL used by the Obsidian plugin, and run the check. It verifies:

- the local sync API;
- public DNS and Cloudflare Tunnel edge DNS;
- `cloudflared /ready` when it is reachable from the container;
- public `/healthz`, Error 1033, Origin failures, and Cloudflare Access responses.

If Cloudflare Access protects the hostname, you may enter a Service Token for this single check. The Client ID and Secret are not persisted. The public URL is stored only in the current browser.

### What Error 1033 means

Error 1033 means that Cloudflare cannot find an available Tunnel connector. It does not necessarily mean that the Go service or Docker container has stopped.

Check each layer from PowerShell on the server computer:

```powershell
curl.exe http://127.0.0.1:8787/healthz
Get-Service cloudflared
curl.exe -i http://127.0.0.1:20241/ready
Resolve-DnsName region1.v2.argotunnel.com
```

`/ready` returns `200` when the connector is ready and `503` when the process exists without an active Tunnel. DNS results in `198.18.0.0/15` or `fdfe:dcba:9876::/64` usually mean that Clash Verge TUN/Fake-IP intercepted the connector hostname.

### Persistent Clash Verge TUN fix

Open **Subscriptions -> Global extension script -> Script** in Clash Verge Rev and use:

```javascript
function main(config) {
  const directRules = [
    "PROCESS-NAME,cloudflared.exe,DIRECT",
    "DOMAIN-SUFFIX,argotunnel.com,DIRECT",
  ];

  const oldRules = Array.isArray(config.rules) ? config.rules : [];
  config.rules = directRules.concat(
    oldRules.filter((rule) => !directRules.includes(rule)),
  );

  if (!config.dns) {
    config.dns = {};
  }

  const realIpPattern = "+.argotunnel.com";
  const oldFilter = Array.isArray(config.dns["fake-ip-filter"])
    ? config.dns["fake-ip-filter"]
    : [];

  if (!oldFilter.includes(realIpPattern)) {
    config.dns["fake-ip-filter"] = [realIpPattern].concat(oldFilter);
  }

  return config;
}
```

Reactivate the subscription while keeping TUN enabled, then restart the connector from an elevated PowerShell:

```powershell
Restart-Service cloudflared
```

Verify that Tunnel DNS no longer returns a Fake-IP, `/ready` returns `200`, and the public `/healthz` returns `status: ok`.

If Error 1033 remains, verify in Cloudflare Zero Trust that the Connector is Healthy, the Public Hostname belongs to the current Tunnel, and its Origin Service is `http://127.0.0.1:8787`. Also confirm that the firewall permits outbound `cloudflared` traffic and reinstall the connector if its Tunnel Token was replaced.

Never publish a Tunnel Token in command output, screenshots, logs, or issues.

## The local management page does not open

The management page is <http://127.0.0.1:8788/admin/>. Confirm that Docker Desktop is running and that the `obsidian-sync-server` container is healthy. Port `8788` must not be added to Cloudflare Tunnel.
