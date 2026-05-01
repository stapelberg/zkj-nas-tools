{
  pkgs,
  ...
}:

pkgs.buildGoModule {
  pname = "dramaqueen";
  version = "unstable";

  src = ./..;

  vendorHash = "sha256-NIL428MGhHNwJJSBXj8fIvjQb/x4A6YG50N9pIMDUjY=";
}
