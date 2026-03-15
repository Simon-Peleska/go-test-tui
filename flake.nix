{
  description = "go-test-tui — terminal UI test runner for Go projects";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};

        go-test-tui = pkgs.buildGoModule {
          pname = "go-test-tui";
          version = "0.0.1";
          src = ./.;

          # After updating Go dependencies, recompute by setting pkgs.lib.fakeHash,
          # running `nix build`, and replacing with the hash from the error output.
          vendorHash = "sha256-XJetiMPWXWnXgoLvwi5it/7FUeiBEoiofACE0nwwvlg=";

          nativeBuildInputs = [ pkgs.installShellFiles ];

          postInstall = ''
            installShellCompletion \
              --cmd go-test-tui \
              --bash ${./completions.bash}
          '';
        };
      in {
        packages.default = go-test-tui;

        apps.default = {
          type = "app";
          program = "${go-test-tui}/bin/go-test-tui";
        };

        devShells.default = pkgs.mkShell {
          packages = [
            pkgs.go
            pkgs.vhs  # record terminal sessions: vhs demo.tape
            go-test-tui
          ];
        };
      });
}
