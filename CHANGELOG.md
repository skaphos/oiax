# Changelog

## [1.1.0](https://github.com/skaphos/oiax/compare/v1.0.3...v1.1.0) (2026-07-18)


### Features

* **backflow:** durable forge-side conflict artifact (SKA-601) ([#37](https://github.com/skaphos/oiax/issues/37)) ([d67c70d](https://github.com/skaphos/oiax/commit/d67c70dfd645637dd2ffd1fa93fee166d314129f))
* **backflow:** merge-commit backflow strategy — foundation (SKA-599) ([#34](https://github.com/skaphos/oiax/issues/34)) ([9834101](https://github.com/skaphos/oiax/commit/9834101e813e319e28b662307cb39b3e168945cd))
* **backflow:** merge-commit execution, fences, and plan output (SKA-599) ([#35](https://github.com/skaphos/oiax/issues/35)) ([1c56544](https://github.com/skaphos/oiax/commit/1c56544ab65d2fe1109a8570eed9736226149a5c))
* **plan:** per-edge equivalence-ladder diagnostics in plan output (SKA-600) ([#30](https://github.com/skaphos/oiax/issues/30)) ([e9139e4](https://github.com/skaphos/oiax/commit/e9139e47730be62bccd998ce87a448665a904d12))
* run Oiax from Azure Pipelines + Azure DevOps forge provider (CI-neutral core, template, provider) ([#38](https://github.com/skaphos/oiax/issues/38)) ([2342c23](https://github.com/skaphos/oiax/commit/2342c23c1568e08c4483fc68a3bd0532ee59cf12))

## [1.0.3](https://github.com/skaphos/oiax/compare/v1.0.2...v1.0.3) (2026-07-13)


### Bug Fixes

* **reconcile:** resolve origin-tracking refs in backflow identity lookup ([83dda24](https://github.com/skaphos/oiax/commit/83dda24bd892702fd6e2e80f7878a0a9461dbac0))

## [1.0.2](https://github.com/skaphos/oiax/compare/v1.0.1...v1.0.2) (2026-07-13)


### Documentation

* **release:** document GitHub Marketplace publishing ([ea6f8f6](https://github.com/skaphos/oiax/commit/ea6f8f6802cf428f06eea4797df1a856cabc6721))

## [1.0.1](https://github.com/skaphos/oiax/compare/v1.0.0...v1.0.1) (2026-07-13)


### Bug Fixes

* **action:** satisfy GitHub Marketplace listing rules ([#20](https://github.com/skaphos/oiax/issues/20)) ([7f126a4](https://github.com/skaphos/oiax/commit/7f126a47710e69250cb56e2646c47f5a130ffb02))

## 1.0.0 (2026-07-12)


### ⚠ BREAKING CHANGES

* the Go import path pkg/api/v1alpha1 moves to pkg/api/v1. Existing .oiax.yaml files keep working (v1alpha1 is accepted as a deprecated alias); only Go programs importing the config types must update the path.

### Features

* **cli:** consistent output, aligned exit codes, and early validation ([#14](https://github.com/skaphos/oiax/issues/14)) ([2f74e46](https://github.com/skaphos/oiax/commit/2f74e4648c63ec282467c12c67b6df54cb2f8cc3))
* **cli:** default --config-ref to the repository default branch ([#5](https://github.com/skaphos/oiax/issues/5)) ([b9b0145](https://github.com/skaphos/oiax/commit/b9b0145a93d4bf83ba67cbc416a9de503b53c47e))
* config API v1 and Action ref resolution (SKA-544/545/546) ([#8](https://github.com/skaphos/oiax/issues/8)) ([410bdd7](https://github.com/skaphos/oiax/commit/410bdd7148aa74a77d11328665d91863d16fee2d))
* implement v0.1 core (SKA-538/539/540/541) ([#3](https://github.com/skaphos/oiax/issues/3)) ([f8fff29](https://github.com/skaphos/oiax/commit/f8fff295d702c7eef2f10233b3aa54c8554cf738))
* implement v0.2 backflow (SKA-542) ([#6](https://github.com/skaphos/oiax/issues/6)) ([74aca51](https://github.com/skaphos/oiax/commit/74aca519f5c6f43b1b0a7756503ba1a52f156127))
* mergeMethod repo-settings warning, harness hermeticity & 1.0 cleanups ([#16](https://github.com/skaphos/oiax/issues/16)) ([a6ba50f](https://github.com/skaphos/oiax/commit/a6ba50ffdbc99c64516347ae77836d28ead5ac1c))
* scaffold oiax repository ([#1](https://github.com/skaphos/oiax/issues/1)) ([0524a28](https://github.com/skaphos/oiax/commit/0524a28a5b43fbb72272878b26325280d2c0183a))


### Bug Fixes

* **action:** advertise Linux runner support only ([#18](https://github.com/skaphos/oiax/issues/18)) ([23e208d](https://github.com/skaphos/oiax/commit/23e208d6fb8aa21ec7353461e04d72d3577bc57d))
* **backflow:** robustness under shallow clone & concurrent target advance ([#15](https://github.com/skaphos/oiax/issues/15)) ([28f814f](https://github.com/skaphos/oiax/commit/28f814fb9decda1f1b8226c3f75f28fa429c4c4e))
* enforce git version floor and harden forge identity (SKA-551/553) ([#12](https://github.com/skaphos/oiax/issues/12)) ([66c11bf](https://github.com/skaphos/oiax/commit/66c11bfe00a8f392c2f6b24ef869f2705dc469b3))
* **forge:** harden GitHub provider — resilience, scale & security ([#13](https://github.com/skaphos/oiax/issues/13)) ([80710f1](https://github.com/skaphos/oiax/commit/80710f1965d2fbeab21ef43138789890c472e759))
* harden backflow against silent hotfix loss (SKA-547/549/550) ([#10](https://github.com/skaphos/oiax/issues/10)) ([f9f506e](https://github.com/skaphos/oiax/commit/f9f506e080f424f9f7c93eb61bc88c5e4e634c51))
* stabilize and document planFormatVersion:1 JSON contract (SKA-548/552) ([#11](https://github.com/skaphos/oiax/issues/11)) ([63d7728](https://github.com/skaphos/oiax/commit/63d7728aa563df696de22afe77196adc0857c752))

## Changelog
