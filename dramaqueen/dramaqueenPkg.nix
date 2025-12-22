{
  pkgs,
  ...
}:

pkgs.buildGoModule {
  pname = "dramaqueen";
  version = "unstable";

  src = ./..;

  vendorHash = "sha256-xnkotgRH82UtTzkI8SO6g36zZ/KbgteAfQI51wUrF2g=";
}
