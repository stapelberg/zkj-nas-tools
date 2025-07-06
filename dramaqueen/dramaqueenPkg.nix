{
  pkgs,
  ...
}:

pkgs.buildGoModule {
  pname = "dramaqueen";
  version = "unstable";

  src = ./..;

  vendorHash = "sha256-DaioL4CykrKQ7+V5lxG7R2AkCOhshHlAvBFkUsXG11w=";
}
