# ermite-actions

> Read-only mirror. Development happens at git.bnema.dev; issues and pull requests are closed here.

The GitHub Actions workflow language in Go: expressions `${{ }}`, template tokens and the workflow schema, the workflow model, matrices, job graphs and trigger filters, and `action.yml` manifests (`action`) with the `uses:` parser. [ermite](https://github.com/bnema/ermite) plans runs with it and [ermite-runner](https://github.com/bnema/ermite-runner) executes jobs with it.

The goal is GitHub's exact behaviour. Semantics and test cases come from GitHub's MIT sources ([actions/languageservices](https://github.com/actions/languageservices), [actions/runner](https://github.com/actions/runner)); see [THIRD_PARTY.md](THIRD_PARTY.md).

Status: early development, no stable API.

## Test

```sh
go test -race ./...
```

## License

MIT. See [LICENSE](LICENSE).
