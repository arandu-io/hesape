# Third-party notices

Arandu is MIT licensed (see `LICENSE.md`). This file covers the third-party
work that is **embedded** in this package and therefore redistributed inside
every binary that links it.

## SecLists 10k-most-common — `common_passwords.txt`

| | |
|---|---|
| Source | https://github.com/danielmiessler/SecLists/blob/master/Passwords/Common-Credentials/10k-most-common.txt |
| Author | Daniel Miessler and the SecLists contributors |
| Taken at | commit 913b327317496d062bcc7cace524aaad8a693be2 (2026-09-08) |
| License | MIT |

The file is the upstream list byte for byte: ten thousand passwords, one per
line. It is read by `CommonPasswords`, which refuses a password that is on it
without asking anybody over the network.

`common_passwords_br.txt` is this project's own addition (passwords common in
Brazil) and carries no third-party text.

```
MIT License

Copyright (c) 2018 Daniel Miessler

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
