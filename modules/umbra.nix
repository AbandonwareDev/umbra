{ config, lib, pkgs, ... }:

with lib;

let
  cfg = config.services.umbra;

  # Automatically provide the overlay so pkgs.umbra and pkgs.umbra-headless
  # are available when this module is imported.
  umbraOverlay = final: prev: {
    umbra = final.callPackage ../pkgs/umbra/default.nix {};
    umbra-headless = final.callPackage ../pkgs/umbra/default.nix { buildHeadless = true; };
  };
in {
  options.services.umbra = {
    enable = mkEnableOption "Umbra VPN controller daemon";

    package = mkOption {
      type = types.package;
      default = pkgs.umbra;
      defaultText = literalExpression "pkgs.umbra";
      description = "Umbra package to use (defaults to full build with TUI/tray).";
    };

    vpnDir = mkOption {
      type = types.str;
      default = "/var/lib/umbra/configs";
      defaultText = literalExpression ''"/var/lib/umbra/configs"'';
      description = "Directory with VPN config files (StateDirectory-managed).";
    };

    configFile = mkOption {
      type = types.nullOr types.path;
      default = null;
      description = "Path to extension-mapping config file (null = built-in defaults).";
    };

    logFile = mkOption {
      type = types.nullOr types.path;
      default = null;
      description = "Path to log file (null = journald).";
    };

    allowUser = mkOption {
      type = types.nullOr types.str;
      default = null;
      description = "Username allowed to control daemon via IPC.";
    };

    trustedPrefixes = mkOption {
      type = types.listOf types.str;
      default = [
        "/nix/store/"
        "/run/wrappers/bin/"
        "/run/current-system/sw/bin/"
        "/bin/"
        "/sbin/"
        "/usr/bin/"
        "/usr/sbin/"
        "/usr/local/bin/"
      ];
      description = "Allowed command path prefixes (must match -trusted-prefixes defaults).";
    };

    noConfig = mkOption {
      type = types.bool;
      default = true;
      description = "Skip config.yaml — built-in defaults only (more secure). Ignored when configFile is set.";
    };

    extraArgs = mkOption {
      type = types.listOf types.str;
      default = [ ];
      description = "Extra CLI arguments passed to the daemon.";
    };
  };

  config = mkMerge [
    # Unconditional: always add the overlay when this module is imported.
    { nixpkgs.overlays = [ umbraOverlay ]; }

    (mkIf cfg.enable {
      systemd.services.umbra = {
        description = "Umbra VPN Controller Daemon";
        after = [ "network.target" ];
        wantedBy = [ "multi-user.target" ];

        serviceConfig = {
          User = "root";
          Type = "simple";
          Restart = "on-failure";
          RestartSec = "5s";
          ExecStart = ''
            ${cfg.package}/bin/umbra daemon -no-tray \
              -vpn-dir ${cfg.vpnDir} \
              -trusted-prefixes ${lib.concatStringsSep "," cfg.trustedPrefixes} \
              ${lib.optionalString (cfg.configFile != null) "-config ${cfg.configFile}"} \
              ${lib.optionalString (cfg.configFile == null && cfg.noConfig) "-no-config"} \
              ${lib.optionalString (cfg.logFile != null) "-log ${cfg.logFile}"} \
              ${lib.optionalString (cfg.allowUser != null) "-allow-user ${cfg.allowUser}"} \
              ${lib.escapeShellArgs cfg.extraArgs}
          '';
          StateDirectory = [ "umbra" ];
          StateDirectoryMode = "0700";
          NoNewPrivileges = true;
          ProtectHome = true;
          ProtectSystem = "strict";
          PrivateTmp = true;
          CapabilityBoundingSet = "";
        };
      };

      # Ensure vpnDir exists
      systemd.tmpfiles.rules = [
        "d ${cfg.vpnDir} 0755 root root -"
      ];

      # Informational warning about VPN packages
      warnings = optional (cfg.enable) ''
        Umbra VPN daemon is enabled. Ensure VPN client packages (e.g., wireguard-tools, openvpn)
        are installed if needed. Set services.umbra.package to configure the umbra binary.
      '';
    })
  ];
}
