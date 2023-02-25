{ pkgs ? import (fetchTarball "https://github.com/NixOS/nixpkgs/archive/7d0ed7f2e5aea07ab22ccb338d27fbe347ed2f11.tar.gz") {}
, gitVersion ? "latest"
}:

let
  buildGit = nixpkgsCommit: relPath: args:
    let
      defaultArgs = {
        guiSupport = false;
        sendEmailSupport = false;
        svnSupport = false;
        withManual = false;
        perlLibs = [pkgs.perlPackages.LWP pkgs.perlPackages.URI pkgs.perlPackages.TermReadKey];
        smtpPerlLibs = [];
      };
      called = pkgs.callPackage
        ((fetchTarball "https://github.com/NixOS/nixpkgs/archive/${nixpkgsCommit}.tar.gz") + "/pkgs/applications/version-management/" + relPath)
        (defaultArgs // args);
    in
      called.overrideAttrs (new: old: {
        doCheck = false;
        doInstallCheck = false;
      });

  gits = {
    "2.17.1" = buildGit
      "9db1f486e15107e417b63119ad5e1917ee126599"
      "git-and-tools/git"
      {
        stdenv = pkgs.stdenv // { inherit (pkgs) lib; };
        python = pkgs.python3;
      };
    "2.20.1" = (buildGit
      "fc917e5346eb7e8858a67dd683be2e43a165918a"  # Git 2.21.0
      "git-and-tools/git"
      {
        stdenv = pkgs.stdenv // { inherit (pkgs) lib; };
        python = pkgs.python3;
      }).overrideAttrs (new: old: {
        name = "git-2.20.1";
        src = pkgs.fetchurl {
          url = "https://www.kernel.org/pub/software/scm/git/git-2.20.1.tar.xz";
          hash = "sha256-nS6R4vqi6mG6CnAgHQI7NvVNhGMUWRoALGEOoquBw+k=";
        };
      });
    "2.25.1" = buildGit
      "b2a903a3e7ac9c038ed5f6a3ee744496622e0b65"
      "git-and-tools/git"
      {
        stdenv = pkgs.stdenv // { inherit (pkgs) lib; };
      };
    "2.27.0" = (buildGit
      "98c44f565746165a556953cda769d23d732466f4"
      "git-and-tools/git"
      {
        stdenv = pkgs.stdenv // { inherit (pkgs) lib; };
      }).overrideAttrs (new: old: {
        outputs = [ "out" ];
      });
    latest = pkgs.git;
  };
in

pkgs.mkShell {
  packages = [
    pkgs.go_1_20
    (builtins.getAttr gitVersion gits)
  ];
}
