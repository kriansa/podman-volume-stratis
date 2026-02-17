# Changelog

## [1.1.1](https://github.com/kriansa/podman-volume-stratis/compare/v1.1.0...v1.1.1) (2026-02-17)


### Bug Fixes

* handle file.Close() error in procmounts parser ([22d3983](https://github.com/kriansa/podman-volume-stratis/commit/22d3983c1618de0475b465342d9868ffefe4a3fa))
* use correct glob patterns for RPM artifacts ([d183f69](https://github.com/kriansa/podman-volume-stratis/commit/d183f69a9f16c752976fbb7c7185d6259a0e4edb))

## [1.1.0](https://github.com/kriansa/podman-volume-stratis/compare/v1.0.2...v1.1.0) (2026-02-16)


### Features

* add beta release support via toggle-prerelease ([3ce5b58](https://github.com/kriansa/podman-volume-stratis/commit/3ce5b58bedd250f9b08b8ab5682744a5af70c766))


### Bug Fixes

* work around stratisd ordering cycle with systemd override ([6b505e4](https://github.com/kriansa/podman-volume-stratis/commit/6b505e4c5155023161a8d4c6745be098b83d082f))

## [1.1.0-beta.1](https://github.com/kriansa/podman-volume-stratis/compare/v1.1.0-beta...v1.1.0-beta.1) (2026-02-16)


### Bug Fixes

* include stratisd ordering workaround files in RPM package ([03e218e](https://github.com/kriansa/podman-volume-stratis/commit/03e218e49f85ab065e76ec4274c9c5887f25cfaf))

## [1.1.0-beta](https://github.com/kriansa/podman-volume-stratis/compare/v1.0.2...v1.1.0-beta) (2026-02-16)


### Features

* add beta release support via toggle-prerelease ([3ce5b58](https://github.com/kriansa/podman-volume-stratis/commit/3ce5b58bedd250f9b08b8ab5682744a5af70c766))


### Bug Fixes

* add versioning strategy for prerelease support ([2b1dd13](https://github.com/kriansa/podman-volume-stratis/commit/2b1dd13efb50c1e1e18215c9b4069eeb7ccaf8cc))
* work around stratisd ordering cycle with systemd override ([6b505e4](https://github.com/kriansa/podman-volume-stratis/commit/6b505e4c5155023161a8d4c6745be098b83d082f))

## [1.0.2](https://github.com/kriansa/podman-volume-stratis/compare/v1.0.1...v1.0.2) (2026-02-16)


### Bug Fixes

* remove redundant systemd dependency directives ([3c6b934](https://github.com/kriansa/podman-volume-stratis/commit/3c6b934bfd0be50193bdcf345a123482e214bdfe))

## [1.0.1](https://github.com/kriansa/podman-volume-stratis/compare/v1.0.0...v1.0.1) (2026-02-16)


### Bug Fixes

* resolve systemd ordering cycle with stratisd ([#3](https://github.com/kriansa/podman-volume-stratis/issues/3)) ([9da7868](https://github.com/kriansa/podman-volume-stratis/commit/9da78681e0ee3088c9fbe965fea39aee889d9a21))

## 1.0.0 (2026-01-15)


### Features

* initial commit ([ddf52e1](https://github.com/kriansa/podman-volume-stratis/commit/ddf52e109713c0993cb74c9fde189181db450043))
