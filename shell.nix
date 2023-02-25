{ pkgs ? import (fetchTarball "https://github.com/NixOS/nixpkgs/archive/7d0ed7f2e5aea07ab22ccb338d27fbe347ed2f11.tar.gz") {}
, gitVersion ? "latest"
}:

let
  defaultArgs = {
    guiSupport = false;
    sendEmailSupport = false;
    svnSupport = false;
    perlLibs = [pkgs.perlPackages.LWP pkgs.perlPackages.URI pkgs.perlPackages.TermReadKey];
    smtpPerlLibs = [];
  };
  callPast = commit: relPath: args: pkgs.callPackage
    ((fetchTarball "https://github.com/NixOS/nixpkgs/archive/${commit}.tar.gz") + "/" + relPath)
    (defaultArgs // args);

  gits = {
    "2.17.1" = callPast
      "9db1f486e15107e417b63119ad5e1917ee126599"
      "pkgs/applications/version-management/git-and-tools/git"
      {
        stdenv = pkgs.stdenv // { inherit (pkgs) lib; };
        python = pkgs.python3;
      };
    "2.25.1" = callPast
      "b2a903a3e7ac9c038ed5f6a3ee744496622e0b65"
      "pkgs/applications/version-management/git-and-tools/git"
      {
        stdenv = pkgs.stdenv // { inherit (pkgs) lib; };
      };
    latest = pkgs.git;
  };
in

pkgs.mkShell {
  packages = [
    pkgs.go_1_20
    (builtins.getAttr gitVersion gits)
  ];
}
