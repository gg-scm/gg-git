{
  description = "gg-scm.io/pkg/git Go package";

  inputs = {
    nixpkgs.url = "nixpkgs";
    nixpkgs-git_2_17_1 = {
      url = "nixpkgs/9db1f486e15107e417b63119ad5e1917ee126599";
      flake = false;
    };
    nixpkgs-git_2_21_0 = {
      url = "nixpkgs/fc917e5346eb7e8858a67dd683be2e43a165918a";
      flake = false;
    };
    nixpkgs-git_2_25_1 = {
      url = "nixpkgs/b2a903a3e7ac9c038ed5f6a3ee744496622e0b65";
      flake = false;
    };
    nixpkgs-git_2_27_0 = {
      url = "nixpkgs/98c44f565746165a556953cda769d23d732466f4";
      flake = false;
    };
    nixpkgs-git_2_45_2 = {
      url = "nixpkgs/53054089b25f3a55c8ca7af466223b94e80941b6";
    };
    nixpkgs-git_2_46_1 = {
      url = "nixpkgs/892b7e93c849a214efb4a689ed1aa310b0bfa95e";
    };

    git_2_20_1 = {
      url = "https://www.kernel.org/pub/software/scm/git/git-2.20.1.tar.xz";
      flake = false;
    };

    flake-utils.url = "flake-utils";

    flake-compat = {
      url = "github:edolstra/flake-compat";
      flake = false;
    };
  };

  outputs = { self, nixpkgs, flake-utils, ... }@inputs:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };
      in {
        packages.go = pkgs.go_1_23;

        packages.git = pkgs.git;

        packages.git_2_17_1 = self.lib.buildGit {
          inherit pkgs;
          packagePath = "${inputs.nixpkgs-git_2_17_1}/pkgs/applications/version-management/git-and-tools/git";
          args = {
            stdenv = pkgs.stdenv // { inherit (pkgs) lib; };
            python = pkgs.python3;
          };
        };

        packages.git_2_20_1 = (self.lib.buildGit {
            inherit pkgs;
            packagePath = "${inputs.nixpkgs-git_2_21_0}/pkgs/applications/version-management/git-and-tools/git";
            args = {
              stdenv = pkgs.stdenv // { inherit (pkgs) lib; };
              python = pkgs.python3;
            };
          }).overrideAttrs (new: old: {
            name = "git-2.20.1";
            src = inputs.git_2_20_1;
          });

        packages.git_2_25_1 = self.lib.buildGit {
          inherit pkgs;
          packagePath = "${inputs.nixpkgs-git_2_25_1}/pkgs/applications/version-management/git-and-tools/git";
          args = {
            stdenv = pkgs.stdenv // { inherit (pkgs) lib; };
          };
        };

        packages.git_2_27_0 = (self.lib.buildGit {
            inherit pkgs;
            packagePath = "${inputs.nixpkgs-git_2_27_0}/pkgs/applications/version-management/git-and-tools/git";
            args = {
              stdenv = pkgs.stdenv // { inherit (pkgs) lib; };
            };
          }).overrideAttrs (new: old: {
            outputs = [ "out" ];
          });

        packages.git_2_45_2 = (self.lib.buildGit {
          inherit pkgs;
          packagePath = "${inputs.nixpkgs-git_2_45_2}/pkgs/applications/version-management/git";
        });

        packages.git_2_46_1 = (self.lib.buildGit {
          inherit pkgs;
          packagePath = "${inputs.nixpkgs-git_2_46_1}/pkgs/applications/version-management/git";
        });

        devShells.default = pkgs.mkShell {
          packages = [
            self.packages.${system}.go
            pkgs.git
          ];
        };
      }
    ) // {
      lib.buildGit = { pkgs, packagePath, args ? {} }:
        let
          defaultArgs = {
            inherit (pkgs.darwin.apple_sdk.frameworks) CoreServices Security;

            guiSupport = false;
            sendEmailSupport = false;
            svnSupport = false;
            withManual = false;
            perlLibs = [pkgs.perlPackages.LWP pkgs.perlPackages.URI pkgs.perlPackages.TermReadKey];
            smtpPerlLibs = [];
          };
          called = pkgs.callPackage packagePath (defaultArgs // args);
        in
          called.overrideAttrs (new: old: {
            doCheck = false;
            doInstallCheck = false;
          });
    };
}
