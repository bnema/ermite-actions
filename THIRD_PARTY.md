# Third-party material

Test fixtures and schemas under `testdata/`, `workflow/testdata/` and `template/schema/` are copied or ported from the projects below. Each copied file names its source in its first lines or in a `SOURCE` file next to it.

## actions/languageservices

https://github.com/actions/languageservices

Used: `expr/testdata/languageservices/*.json` (the cross-language expression fixtures, commit 4043eda158e16579cc5fb1b0b07a4bce2a76f0b5) and the `@actions/expressions` package that the other `expr` oracle files were generated from.

```
MIT License

Copyright GitHub, Inc.

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

## actions/runner

https://github.com/actions/runner — MIT License, Copyright (c) GitHub, Inc. Same terms as above.
