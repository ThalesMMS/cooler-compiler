# Third-party notices

This document records the origin and license status of dependencies, platform components, assets, and copied teaching material used by Cooler Compiler. Project-owned material is covered by [`NOTICE`](NOTICE) and [`LICENSE`](LICENSE); the entries below retain their own terms.

Release packaging copies `LICENSE`, `NOTICE`, and this file alongside distribution binaries and into the macOS application's `Contents/Resources` directory.

## Go runtime and standard library

Release binaries include portions of the Go runtime and standard library from [The Go Programming Language](https://go.dev/). The exact toolchain version is selected by the builder; the project currently declares Go 1.25.0 in `go.mod`.

- Source: <https://go.googlesource.com/go/>
- License: BSD 3-Clause-style license reproduced below
- Copyright: Copyright 2009 The Go Authors

```text
Copyright 2009 The Go Authors.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are
met:

   * Redistributions of source code must retain the above copyright
notice, this list of conditions and the following disclaimer.
   * Redistributions in binary form must reproduce the above
copyright notice, this list of conditions and the following disclaimer
in the documentation and/or other materials provided with the
distribution.
   * Neither the name of Google LLC nor the names of its
contributors may be used to endorse or promote products derived from
this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
"AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

## Native Windows WebView dependency

Native Windows builds link [`github.com/webview/webview_go`](https://github.com/webview/webview_go) at commit [`6173450d4dd61511002d897d55e4d0b6e75aeb96`](https://github.com/webview/webview_go/tree/6173450d4dd61511002d897d55e4d0b6e75aeb96), recorded in `go.mod` as `v0.0.0-20240831120633-6173450d4dd6`.

### webview_go bindings

- License: MIT
- Copyright: Copyright (c) 2017 Serge Zaitsev; Copyright (c) 2020 webview

```text
MIT License

Copyright (c) 2017 Serge Zaitsev
Copyright (c) 2020 webview

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

### Embedded webview C/C++ core

The Go module vendors the [webview C/C++ library](https://github.com/webview/webview) at commit [`fb6b17d826041411e6346cd9a785a5ceba7987c4`](https://github.com/webview/webview/tree/fb6b17d826041411e6346cd9a785a5ceba7987c4).

- License: MIT
- Copyright: Copyright (c) 2017 Serge Zaitsev; Copyright (c) 2022 Steffen André Langnes

```text
MIT License

Copyright (c) 2017 Serge Zaitsev
Copyright (c) 2022 Steffen André Langnes

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

### Microsoft WebView2 SDK files

The Go module vendors WebView2 SDK interface files from `Microsoft.Web.WebView2` version `1.0.1150.38`. The WebView2 Runtime itself is a platform prerequisite and is not stored in this repository or application bundle.

- Source: [Microsoft Edge WebView2](https://developer.microsoft.com/microsoft-edge/webview2/)
- License: license shipped with the vendored SDK files, reproduced below
- Copyright: Copyright (C) Microsoft Corporation. All rights reserved.

```text
Copyright (C) Microsoft Corporation. All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are
met:

   * Redistributions of source code must retain the above copyright
notice, this list of conditions and the following disclaimer.
   * Redistributions in binary form must reproduce the above
copyright notice, this list of conditions and the following disclaimer
in the documentation and/or other materials provided with the
distribution.
   * The name of Microsoft Corporation, or the names of its contributors
may not be used to endorse or promote products derived from this
software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
"AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

## Platform WebViews not bundled

- macOS native builds use Apple's system `WebKit`/`WKWebView` framework. It is supplied by macOS and is not copied into the repository or application bundle.
- Windows native builds require the Microsoft Edge WebView2 Runtime. The runtime is installed separately and is governed by Microsoft's terms.
- CGO-disabled builds and supported non-macOS/non-Windows builds open the system's default browser and do not bundle a browser engine.

## Bundled COOL teaching material

The following files originated in the PUC Minas “Compiladores Parte 1” teaching materials, which follow the classic Stanford/UC Berkeley COOL stack-machine assignment:

- `examples/13_stack_machine.cl`
- `examples/stack-machine/atoi.cl`
- `examples/stack-machine/stack.test`

The assignment family and COOL distribution are described by [Stanford CS143](https://web.stanford.edu/class/cs143/) and the [UC Berkeley COOL distribution page](https://ftp.eecs.berkeley.edu/cool/). The `atoi.cl` helper is copied from the COOL examples collection; the stack interpreter and input are the corresponding course solution and test data.

No standalone open-source license or SPDX identifier accompanied these files, and no public license grant was identified for them. They are not claimed as project-owned work or relicensed under the project's AGPL declaration; all rights remain with their respective authors or institutions. They are retained in the source tree for assignment compatibility and are not included in binary release packages.

## Project-owned assets

The following assets were created specifically for Cooler Compiler and are covered by the project's `AGPL-3.0-or-later` license:

- `assets/AppIcon.png`: source/master application artwork, first committed with the project About dialog and packaging support;
- `assets/AppIcon.icns`: macOS icon bundle derived from `assets/AppIcon.png`;
- `internal/web/static/app-icon.png`: 256×256 web derivative of `assets/AppIcon.png`;
- `internal/web/static/favicon.svg`: original Cooler “C” favicon artwork.

No third-party fonts, icon packs, JavaScript packages, stylesheets, or remote assets are bundled by the web interface.
