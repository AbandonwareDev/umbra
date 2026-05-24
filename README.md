# Umbra — VPN controller daemon + TUI + tray

Daemon + TUI + tray. Monitors a folder for different VPN configs. One daemon to rule all VPN services

## Quick start

```bash
go install .
sudo umbra daemon -no-config -vpn-dir /yourVPNsFolder -allow-user $USER
umbra tui
umbra tray          # standalone tray, separate terminal
```

## Modes

**User mode** (`umbra daemon`): socket at `/tmp/umbra-$UID/daemon.sock`, tray enabled.  
VPNs that exit within 10s (likely need root) are re-launched via `pkexec` (max 2 retries, 1s delay).

**Root mode** (`sudo umbra daemon -allow-user <user>`): socket at `/tmp/umbra/daemon.sock`, tray disabled.  
Access control via `SO_PEERCRED` — only the allowed user and `networkmanager` group members can connect.  
Config folder chowned to root (`0755` dir, `0644` files) — user can read but not write or create files.

> **Security**: In root mode the config folder defaults to `/etc/umbra/configs/` (not `~/.umbra/`)
> so non-root users cannot bypass security through their home directory.

## VPN configs

Place `.ovpn`, `.conf`, `.torrc`, `.sgb`, or `.json` files in the VPN folder.  
Unrecognized extensions are ignored. Configs are sorted by type (OpenVPN → WireGuard → NetworkManager → sing-box → other → Tor), then alphabetically.

Custom commands via `~/.umbra/config.yaml`:

```yaml
extensions:
  .ext: "command --flag {{path}}"
```

`{{path}}` = full config path, `{{name}}` = filename without extension.  
Use `-no-config` to skip the config file (only built-in defaults).

## Working directory

VPN apps may create temp files in the current directory, so the daemon sets its working directory to `/tmp` before launching any VPN process.

