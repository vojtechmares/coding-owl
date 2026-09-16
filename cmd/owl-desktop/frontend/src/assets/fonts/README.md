# Fonts

Geist and Geist Mono, the typefaces codingowl.dev is set in (ADR-0009). They
are bundled rather than fetched from Google Fonts, because a desktop app has to
look the same with the network down.

- Upstream: <https://github.com/vercel/geist-font>
- Licence: SIL Open Font License 1.1
- These files: the `latin` and `latin-ext` subsets Google Fonts serves, taken
  from the `css2` stylesheet the website links. Both are variable fonts
  covering the whole weight axis, which is why there is one file per subset
  rather than one per weight, and why `tokens.css` declares `font-weight: 100
  900` on each face.

To refresh them, read the `src: url(...)` values out of

    https://fonts.googleapis.com/css2?family=Geist&family=Geist+Mono&display=swap

for the `latin` and `latin-ext` unicode ranges, and download those four files
over these. Nothing else needs to change.
