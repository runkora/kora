# Contributing

Thanks for your interest in contributing to kora!

## Ways to contribute

There are more ways to contribute than code. Report a bug, fix the docs, help triage an issue, or answer a question from someone who is stuck.

## Ask for help

If you are unsure, open a GitHub issue.

## Getting set up

Get kora running locally with the [local development guide](docs/contributing/local-development.md). You do not need a running cluster to contribute documentation.

Please read and follow our [Code of Conduct](CODE_OF_CONDUCT.md).

### Tips to get started

- Review the [architecture documentation](ARCHITECTURE.md) to understand how kora works.
- Look through closed issues and PRs for examples of changes similar to yours.

## Filing an issue

To file a bug or feature request for kora:

1. Search existing issues for duplication.
2. If no existing issue covers your case, create a new one.
3. Use an issue template when applicable.

## Finding something to work on

kora uses [GitHub Issues](https://github.com/clear-route/orbit/issues) to track bugs and feature requests. Issues labelled with [good first issue](https://github.com/clear-route/orbit/issues?q=state%3Aopen%20label%3A%22good%20first%20issue%22) are a good place to start. If an issue has not been assigned, you can request to work on it by leaving a comment.

## Contributing a change

For a new feature or a significant change, raise it in an issue and agree the design with us before you write code.

1. Write a PRD from [docs/prd/template.md](docs/prd/template.md) and agree the scope with a maintainer.
2. Write an ADR from [docs/adr/template.md](docs/adr/template.md) capturing the technical decision.
3. Implement against the agreed documents, with tests.

Small patches and bug fixes do not need prior discussion. Open or reference an issue and fix it, with appropriate tests.

## Before you open a PR

- `make check` passes.
- Tests cover the change.
- Documentation reflects the change.

Follow the coding conventions in [docs/conventions.md](docs/conventions.md). Do not add inline `//nolint` or `eslint-disable` directives.

## Commits and pull requests

- Commits follow Conventional Commits with an optional scope, for example `fix(policy): fail closed on unbound and unverified delegation tokens`.
- Reference the ADR that motivates the change in the PR description.
- Branch from `main`. Keep each PR to one change.
- Commits must be signed. `make hooks`, which `make setup` runs, signs your commits with your SSH key and registers that key with GitHub as a signing key through the `gh` CLI. Without `gh`, add the key on GitHub under Settings, SSH and GPG keys, as a **Signing key**.

Contributions are accepted under the [Apache License 2.0](LICENSE).

## Developer Certificate of Origin

Every commit must be signed off under the [Developer Certificate of Origin](https://developercertificate.org/). A sign-off is your statement that you wrote the change or have the right to submit it under the project's licence. It is a line at the end of the commit message with the name and email of the commit's author:

```
Signed-off-by: Jane Doe <jane@example.com>
```

`make hooks` installs a hook that adds the line to every commit. Without the hook, commit with `git commit -s`. The DCO check fails a pull request with any commit that is not signed off. To sign off commits already on your branch, rebase them and push again:

```bash
git rebase --signoff origin/main
git push --force-with-lease
```

## Security

Report vulnerabilities by following the [security policy](SECURITY.md). Do not open a public issue for a suspected vulnerability.
