{
  description = "edge-fabric development and HIL helper shell";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      forAllSystems =
        f:
        nixpkgs.lib.genAttrs systems (
          system:
          f (
            import nixpkgs {
              inherit system;
              config.allowUnfree = false;
            }
          )
        );
    in
    {
      devShells = forAllSystems (
        pkgs:
        let
          lib = pkgs.lib;
          go = if pkgs ? go_1_25 then pkgs.go_1_25 else pkgs.go;
          python = pkgs.python312;
          pythonEnv = python.withPackages (
            ps:
            [
              ps.pip
              ps.pyserial
              ps.pytest
              ps.setuptools
              ps.wheel
            ]
            ++ lib.optionals (ps ? esptool) [
              ps.esptool
            ]
          );
          optionalPackages =
            lib.optionals (pkgs ? esptool) [ pkgs.esptool ]
            ++ lib.optionals (pkgs ? picocom) [ pkgs.picocom ]
            ++ lib.optionals (pkgs ? minicom) [ pkgs.minicom ];
        in
        {
          default = pkgs.mkShell {
            packages =
              [
                go
                pythonEnv
                pkgs.cmake
                pkgs.git
                pkgs.gnumake
                pkgs.jq
                pkgs.ninja
                pkgs.pkg-config
                pkgs.sqlite
                pkgs.usbutils
                pkgs.which
              ]
              ++ optionalPackages;

            GOTOOLCHAIN = "local";
            EDGE_FABRIC_NIX_SHELL = "1";

            shellHook = ''
              echo "edge-fabric Nix shell"
              echo "Go: $(go version 2>/dev/null || true)"
              echo "Python: $(python --version 2>/dev/null || true)"
              echo ""
              echo "Common checks:"
              echo "  go test ./..."
              echo "  python -S scripts/doctor.py --require-go"
              echo "  PYTHONPATH=src python -S -m unittest discover -s tests -v"
              echo ""
              echo "HIL helpers included when available: pyserial, esptool, usbutils, jq, sqlite."
              echo "ESP-IDF itself is intentionally external for now; source export and Go/Python checks are Nix-managed."
            '';
          };
        }
      );

      formatter = forAllSystems (
        pkgs: if pkgs ? nixfmt-rfc-style then pkgs.nixfmt-rfc-style else pkgs.nixfmt
      );
    };
}
