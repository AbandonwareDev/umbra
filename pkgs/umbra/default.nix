{ buildGoModule, lib, buildHeadless ? false }:

let
  args = {
    pname = "umbra";
    version = "unstable-2026-05-24";

    src = ../..;

    vendorHash = "sha256-xteRf93HDdYb6NcuZIHVjEdqgARwyucJd+C4m1RxIXM=";

    meta = with lib; {
      description = "VPN controller daemon + TUI + tray";
      homepage = "https://github.com/AbandonwareDev/umbra";
      license = licenses.gpl3Only;
      platforms = platforms.linux;
      maintainers = [ /* add yourself */ ];
    };
  };
in
if buildHeadless then
  buildGoModule (args // {
    pname = "umbra-headless";
    buildTags = [ "notray" ];
  })
else
  buildGoModule args
