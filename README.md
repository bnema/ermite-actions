<h1 align="center">ermite-actions</h1>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue?style=flat-square" alt="License: MIT"></a>
  <a href="https://github.com/bnema/ermite-actions/commits/main"><img src="https://badgen.net/github/last-commit/bnema/ermite-actions/main?icon=github" alt="Last commit"></a>
  <a href="https://github.com/bnema/ermite-actions/stargazers"><img src="https://badgen.net/github/stars/bnema/ermite-actions?icon=github" alt="GitHub stars"></a>
</p>

<p align="center">The GitHub Actions workflow language in Go.</p>

> Read-only mirror. Development happens at git.bnema.dev; issues and pull requests are closed here.

---

## What it covers

- Expressions `${{ }}`, template tokens and the workflow schema
- Workflow model, matrices, job graphs and trigger filters
- `action.yml` manifests and the `uses:` parser

[ermite](https://github.com/bnema/ermite) plans runs with it; [ermite-runner](https://github.com/bnema/ermite-runner) executes jobs with it.

The target is GitHub's exact behaviour. Semantics and test cases come from GitHub's MIT sources ([actions/languageservices](https://github.com/actions/languageservices), [actions/runner](https://github.com/actions/runner)). See [THIRD_PARTY.md](THIRD_PARTY.md).

Status: early development, no stable API.

## Test

```sh
go test -race ./...
```

## License

MIT. See [LICENSE](LICENSE).
