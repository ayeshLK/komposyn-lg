# Changelog

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
