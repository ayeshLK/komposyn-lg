# Changelog

## [0.11.0](https://github.com/ayeshLK/komposyn-lg/compare/v0.10.0...v0.11.0) (2026-10-24)


### Features

* support concurrent REPL sessions ([ea4c144](https://github.com/ayeshLK/komposyn-lg/commit/ea4c144494353f659838d865cd56d776ac63ed77))


### Bug Fixes

* keep REPL startup responsive during diagnostic review ([25695b7](https://github.com/ayeshLK/komposyn-lg/commit/25695b77ecfd010ef550969ef00ad87e705ffa34))

## [0.10.0](https://github.com/ayeshLK/komposyn-lg/compare/v0.9.0...v0.10.0) (2026-10-21)


### Features

* add project inspection command ([bb25799](https://github.com/ayeshLK/komposyn-lg/commit/bb257990ed66ef0a24d47b8a4549490c2b42412c))
* add project inspection command ([1e09565](https://github.com/ayeshLK/komposyn-lg/commit/1e0956515658302f080bf11422d5f6b13cfaaba1))


### Bug Fixes

* align signed release core version checks ([8025146](https://github.com/ayeshLK/komposyn-lg/commit/802514606e299d363a2660dc8882c7f1dd015d81))
* align signed release core version checks ([0387e40](https://github.com/ayeshLK/komposyn-lg/commit/0387e40691e6fc18f8418e7eb7387b75cc56806f))
* **cli:** skip imports with unknown adapters ([79da60c](https://github.com/ayeshLK/komposyn-lg/commit/79da60c7f29fa35f3d8673eb2ff2bb1782de7f05))
* **cli:** skip imports with unknown adapters ([639c50b](https://github.com/ayeshLK/komposyn-lg/commit/639c50b2c7a471ba3d400bd3157b75f138991bfb))
* fail release preparation on core resolution errors ([08ef8d4](https://github.com/ayeshLK/komposyn-lg/commit/08ef8d41dd931f801d61d91ef97c8662b3b50049))
* validate schema 17 upgrade path ([174b367](https://github.com/ayeshLK/komposyn-lg/commit/174b367b75705432dd42cfbf2d6c9e8d34c863b1))

### Compatibility and upgrade notes

* This CLI release pins KompoSyn core v0.9.0. CLI and core versions remain independent.
* Opening an existing database upgrades it to schema 17 when necessary, after creating a verified pre-migration backup. Older CLI versions that support only earlier schemas cannot open the upgraded database. Preserve the backup; returning to an older binary requires restoring compatible pre-upgrade data separately. There is no automatic down-migration.
* `/projects` continues to list all known projects; its usage measures reflect the selected range. New `/projects inspect <row|id>` and `projects inspect <project-id>` commands distinguish selected-range activity, persisted history, and stored sessions. Inspection JSON uses core format version 2; existing report JSON/CSV and protocol v1 remain unchanged.
* Inspection omits raw identity/provenance text, but stable digests and exact activity metrics remain potentially identifying. Review results and returned errors before sharing.
* Upgrading does not automatically remove zero-usage projects or repair every historical name. Historical name/source provenance can remain unavailable; valid inactive and metadata-only sessions are preserved.

## [0.9.0](https://github.com/ayeshLK/komposyn-lg/compare/v0.8.2...v0.9.0) (2026-10-19)


### Features

* improve interactive REPL clarity ([6189d35](https://github.com/ayeshLK/komposyn-lg/commit/6189d358c76b12b82833ecb96fb5520b86ee4f79))

## [0.8.2](https://github.com/ayeshLK/komposyn-lg/compare/v0.8.1...v0.8.2) (2026-10-19)


### Performance Improvements

* document checkpointed append collection ([1fb95d6](https://github.com/ayeshLK/komposyn-lg/commit/1fb95d61e074d17496eb15f93162f44d56db82b9))
* document checkpointed append collection ([5350ef2](https://github.com/ayeshLK/komposyn-lg/commit/5350ef2297583e0d425b37fed3aab898b911c94f))

## [0.8.1](https://github.com/ayeshLK/komposyn-lg/compare/v0.8.0...v0.8.1) (2026-10-19)


### Performance Improvements

* integrate core v0.7.1 and report [#138](https://github.com/ayeshLK/komposyn-lg/issues/138) CLI measurements ([95d89a1](https://github.com/ayeshLK/komposyn-lg/commit/95d89a15c63660114b4468a3ab22cb1df41eed81))
* validate core v0.7.1 CLI integration ([5e159db](https://github.com/ayeshLK/komposyn-lg/commit/5e159dbc096695e9ed8bc5e255cd27d9055325f2))

## [0.8.0](https://github.com/ayeshLK/komposyn-lg/compare/v0.7.0...v0.8.0) (2026-10-18)


### Features

* extract standalone token logger CLI ([1bd04fa](https://github.com/ayeshLK/komposyn-lg/commit/1bd04fa994bdd323c4a9874fe45dc341bd7f8fbd))


### Bug Fixes

* address standalone PR review feedback ([4cf796b](https://github.com/ayeshLK/komposyn-lg/commit/4cf796b0c2aecb1b009c62a6c274cc514f2091c3))
* address standalone release review blockers ([15e006a](https://github.com/ayeshLK/komposyn-lg/commit/15e006aff87dcff2163ce88ea2dfbaa028608c50))
* select native Windows ARM64 release archives ([79be043](https://github.com/ayeshLK/komposyn-lg/commit/79be04318fa5ee8753d11611652794a46777a5f7))
* use configured token for release preparation ([a19af0c](https://github.com/ayeshLK/komposyn-lg/commit/a19af0c97eb639a559fff8295b6ac6b7f31b7321))
* use plain release tags with Release Please ([87cb7e0](https://github.com/ayeshLK/komposyn-lg/commit/87cb7e00e92115acc9a98c3468f890288ad3a6b8))
* use plain release tags with Release Please ([2c0eb99](https://github.com/ayeshLK/komposyn-lg/commit/2c0eb99ce7e1563bd8fd9caa37f1f5471ade4c03))

## Changelog

Standalone CLI releases are tracked independently from the KompoSyn core.
Release Please prepares entries from Conventional Commits in reviewed pull
requests. Do not manually create or move release tags.

The first standalone release is proposed as v0.7.1. Version 0.7.0 is the
published combined-repository extraction baseline, not a standalone release.
The [combined repository history](https://github.com/ayeshLK/komposyn) records
changes through that baseline; this changelog will contain standalone release
entries from the first release onward.
