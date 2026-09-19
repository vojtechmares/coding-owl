# Issue #128: the desktop app shows the Coding Owl icon, not the Wails default

`cmd/owl-desktop/build/appicon.png` was the Wails scaffold placeholder - a
black "W" on a white rounded square - so the packaged app showed it in the
dock, in the window chrome and in the bundle. The project's own artwork was
already in the repository at `docs/assets/coding-owl-logo.png`, and the website
already ships it reduced as `favicon.png` and `apple-touch-icon.png`.

The change is that `appicon.png` is that artwork at 1024x1024, the size Wails
takes as the master it generates the macOS `.icns` and the Windows `.ico` from
(`Info.plist` names `iconfile`, which Wails writes from this file). Nothing
about the build changes: swapping the PNG is the whole fix.

Owl's artwork is photographic rather than flat, so the scenarios check what a
person would check by eye, mechanically: that the file is the master size Wails
wants, that it is no longer the placeholder, that it really is the project's
logo rather than some other picture, and that it still has structure at the
sizes a dock and a menu bar draw it - the "does it go muddy" question the issue
asks. They compare images by averaging them down to a coarse grid, which is how
two renderings of one picture are told from two different pictures without
demanding they be byte-identical.

Everything but S5 reads files with Go's own `image/png`, so it runs on any
machine. S5 builds the app and is skipped without `OWL_DESKTOP_BUILD`, the way
`tests/behavior/issue-79.md` S4 is.

## Scenarios

### S1 - the icon is where Wails looks for it, at the master size
Given the repository
When `cmd/owl-desktop/build/appicon.png` is read
Then it decodes as a PNG
And it is exactly 1024x1024, the master Wails generates the platform icons from

### S2 - it is no longer the Wails placeholder
Given the icon, and that the placeholder was a black "W" on a white card - two thirds of it near-white, and bright overall
When the icon's own pixels are measured
Then fewer than a fifth of the ones you can see are near-white
And its mean luminance is below 90 of 255
And so it is not that card, without needing the deleted file to compare against

### S3 - it is the Coding Owl logo
Given the icon and `docs/assets/coding-owl-logo.png`
When both are averaged down to a 16x16 grid
Then no cell differs by more than 12 of 255 in any channel
And so the icon is that artwork rather than some other picture

### S4 - it still reads at the sizes a dock and a menu bar draw it
Given the icon
When it is averaged down to 32x32, smaller than any dock draws it
Then it still carries structure rather than going flat: the luminance of its cells varies by more than 20 of 255
And the same holds at 16x16, the size of a menu bar

### S5 - the packaged app carries it
Given the Wails CLI, Node and pnpm, and `OWL_DESKTOP_BUILD` set
When `make desktop` runs
Then the app bundle under `cmd/owl-desktop/build/bin` holds `Contents/Resources/iconfile.icns`
And the largest image inside that `.icns` is the Coding Owl logo by S3's measure
And without `OWL_DESKTOP_BUILD` the scenario is skipped, not failed

### S6 - the build notes say how the icon was made
Given `cmd/owl-desktop/build/README.md`
When it is read
Then it names `docs/assets/coding-owl-logo.png` as the source of `appicon.png`
And it gives the command that derives one from the other, so the icon can be remade rather than guessed at
