# Media Manager build of AltMount

This branch (`media-manager`) is AltMount at upstream commit `2761575`
(`fix(stremio): reject unrelated releases before cache reuse (#1051)`) plus the
fixes below. Media Manager installs the image built from it
(`ghcr.io/media-manager52/altmount`) and manages its Usenet servers from its
own settings page.

## Changes on top of AltMount

- **Wrong-article failover (pool):** when a provider serves a different post
  under a first article's message-ID, the article is refused and fetched from
  the next provider instead of being accepted. Corrected articles are kept in
  `corrected_articles.tsv` next to the database.
- **RAR volume following:** a reposted first volume with a clean release name
  is merged with its scene-named `.rNN` run (by volume size), so old-style
  volume following continues.
- **Parser:** single-file metadata names keep their extension; gzip NZBs are
  detected when retry staging renames them.
- **AES:** `Close` is safe while a `Read` is creating or reading the source.
- **FUSE:** a panic in one FUSE operation fails only that request; an optional
  read-only kernel mount (`fuse.read_only`).
- **Files:** moving a source that is already gone counts as success.
- **Build:** `third_party/nntppool` is vendored (MIT, by Javier Blanco) with
  the pool changes above.
- **CI:** the upstream image and release workflows (which publish to the
  AltMount project's own registries) are replaced by
  `.github/workflows/media-manager-image.yml`.

## License

MIT, as AltMount. See [LICENSE](LICENSE) and `third_party/nntppool/LICENSE`.
