# Bundled fonts

Open-licensed fonts embedded in the page, so every skin draws with the same face on every system and the window fetches nothing. The files are the upstream fonts as Debian 13 packages them, unmodified (not subset, not converted).

| Family          | Files                                                     | Version | Debian 13 package                 | Licence                                                                                                        | Skins                                                          |
| --------------- | --------------------------------------------------------- | ------- | --------------------------------- | -------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------- |
| Inter           | `InterVariable.ttf` (weights 100 to 900)                  | 4.1     | `fonts-inter-variable` 4.1+ds-1   | SIL Open Font License 1.1                                                                                      | Light, Dark, macOS; Ubuntu where the system has no Ubuntu font |
| DejaVu Sans     | `DejaVuSans.ttf`, `DejaVuSans-Bold.ttf`                   | 2.37    | `fonts-dejavu-core` 2.37-8        | Bitstream Vera licence, changes in the public domain                                                           | Windows XP                                                     |
| Liberation Sans | `LiberationSans-Regular.ttf`, `LiberationSans-Bold.ttf`   | 2.1.5   | `fonts-liberation` 1:2.1.5-3      | SIL Open Font License 1.1                                                                                      | Windows 98                                                     |
| JetBrains Mono  | `JetBrainsMono-Regular.ttf`, `JetBrainsMono-SemiBold.ttf` | 2.304   | `fonts-jetbrains-mono` 2.304+ds-5 | Apache License 2.0 as recorded by the package (upstream also publishes it under the SIL Open Font License 1.1) | Matrix                                                         |

`licenses/<family> <version>.txt` is the package's copyright file with the full licence text; `scripts/go-licenses.sh` copies each into the third-party notices of every build. `fonts.css` declares the faces.

To add a font: put the unmodified file and its licence file here, declare it in `fonts.css`, name the family first in the skin's `--font-ui`, add a row above, and add the family the skin must load to `build/qa/offline-skins.sh`.
