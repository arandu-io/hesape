# Third-party notices

Arandu is MIT licensed (see `LICENSE.md`). This file covers the third-party
work that is **embedded** in `view/assets/` and therefore redistributed inside
every binary built with this framework — including binaries built by people who
never downloaded any of it.

That is what makes this file necessary rather than polite. `go:embed` puts these
bytes in the executable, so every user of Arandu becomes a redistributor, and a
redistributor of MIT-licensed code owes the copyright notice. There is no CDN
and no `node_modules` to point at instead (RULE 13): the notice has to travel
with the repository.

Keeping it current is checked, not remembered: `TestEveryEmbeddedAssetIsCredited`
in `view/third_party_test.go` fails when a file appears in `view/assets/`
without an entry here, and when the version recorded here stops matching the
version inside the file.

---

## YPY — `view/assets/ypy.min.js` and `view/assets/ypy-csrf.min.js`

| | |
|---|---|
| Version | 0.1.1 |
| Author | HYZIS - SERVICOS DIGITAIS LTDA - EPP |
| Home | https://github.com/ypy-develop/ypy (tag v0.1.1; the files are `dist/embed/ypy.min.js` and `dist/embed/ext/ypy-csrf.min.js`, the builds without the source map comment) |
| License | MIT |

`ypy.min.js` is the core of YPY (`dist/embed/ypy.min.js`) as it is built, with its
native behaviour and nothing that reproduces another library: morph is the
default swap, the attributes that reach descendants say so with `:inherited`,
and the events are the `ypy:<noun>:<verb>` ones. `ypy-csrf.min.js` is the
extension that puts the token of the `ypy-csrf` meta tag on every request that
changes something (`dist/embed/ext/ypy-csrf.min.js`). YPY is the hypermedia library
Arandu uses; it is not an Arandu package, which is why it is credited here.
Both files carry the same license and the same notice.

```
The MIT License (MIT)

Copyright (c) HYZIS - SERVICOS DIGITAIS LTDA - EPP

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
```

---

## Tailwind CSS — `view/assets/app.css`

| | |
|---|---|
| Version | 4.3.3 |
| Author | Tailwind Labs, Inc. |
| Home | https://tailwindcss.com |
| License | MIT |

`app.css` is compiled output, not a copy of the distribution: the Arandu source
in `view/assets/app.src.css` goes through the standalone `tailwindcss` binary,
and what comes out contains Tailwind's own preflight and utility declarations.
The compiler preserves its banner at the top of the file, which is the version
recorded above:

```
/*! tailwindcss v4.3.3 | MIT License | https://tailwindcss.com */
```

```
MIT License

Copyright (c) Tailwind Labs, Inc.

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

---

## Basecoat — `view/assets/basecoat.bundle.js`

| | |
|---|---|
| Version | 1.0.2 |
| Author | Ronan Berder |
| Home | https://basecoatui.com |
| License | MIT |

The bundle is the concatenation of seven of the component scripts upstream
ships: the registry the others register into, plus the dropdown menu, the
popover, the select, the sidebar, the tabs and the toast. Each is a plain IIFE
with no import and no export, so concatenating them is the whole build.

Its stylesheet is **not** here. That ships with the project rather than with the
framework, vendored under `resources/css/basecoat/`, with this same licence
beside it -- a project owns its design system, and changing how a button looks
should be editing a file you can see rather than upgrading a dependency.

```
MIT License

Copyright (c) 2025 Ronan Berder

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

---

## Arandu's own files

Listed so that every file in `view/assets/` is accounted for, and so that
adding one is a decision somebody wrote down rather than an omission.

- `view/assets/theme.js` — Arandu, MIT, covered by `LICENSE.md`. It reads the
  theme somebody chose out of localStorage and applies it before the first
  paint. It contains no third-party code.
- `view/assets/ui.js` — Arandu, MIT, covered by `LICENSE.md`. It is the
  delegated client behaviour: copy buttons, the theme toggle, the combobox, the
  command palette and the range slider, all dispatched from `data-*` attributes
  read as data. It contains no third-party code, and it is why no directive
  framework is embedded here — compiling an attribute into a function needs
  `unsafe-eval`, and the Content-Security-Policy this framework sets is
  `script-src 'self'`.
- `view/assets/app.src.css` — Arandu, MIT, covered by `LICENSE.md`. It is the
  Tailwind input this project wrote; it contains no third-party code, only
  `@import "tailwindcss"`, which is a build instruction.
