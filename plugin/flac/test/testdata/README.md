# Test Audio Vectors

These files are FLAC conformance test vectors imported from the `ietf-wg-cellar/flac-test-files` repository.

## Source & Version
- **Repository:** https://github.com/ietf-wg-cellar/flac-test-files
- **Commit:** aa7b0c6cf32994c106ae517a08134c28a96ff5b2
- **License:** CC0 1.0 Universal (Public Domain dedication)

## Files List
- `60 - mono audio.flac`: mono audio subset vector.

## Conformance vectors

The complete CC0 decoder testbench is under `conformance/`:

- `subset/`: valid streamable and non-streamable subset vectors 01–64.
- `uncommon/`: valid container FLAC vectors with uncommon properties.
- `faulty/`: malformed/corrupted vectors used for panic-free robustness checks.

The `subset/` vectors are compared against FFmpeg PCM output. The uncommon
vectors are decoded as safety/construction tests because FFmpeg rejects some
streams whose audio properties change between frames.

`conformance/` is the pinned `ietf-wg-cellar/flac-test-files` submodule at
commit `aa7b0c6cf32994c106ae517a08134c28a96ff5b2`. Clone this repository with
`--recurse-submodules` (or run `git submodule update --init --recursive`) to
execute the full conformance test.
