# Contributing

Thanks for wanting to help with kora. Right now the most useful thing you can do is tell us where it falls short, whether that's a bug, a missing feature or a guide that didn't work when you followed it. Code is welcome too, and for anything bigger than a small fix, open an issue first and we'll work out the design with you.

Everyone taking part follows the [Code of Conduct](CODE_OF_CONDUCT.md). Report vulnerabilities privately through the [security policy](SECURITY.md).

## Open an issue

Have a look through [existing issues](https://github.com/runkora/kora/issues) first. If nobody has raised it yet, open a new one for a single problem. A focused issue is much easier for us to pick up than one covering several things.

- **Bug:** the kora version or commit, how kora is installed, the steps that reproduce it, what you expected and what happened. Include logs or error messages.
- **Feature:** the problem and who it affects. Describe the problem before proposing a solution.
- **Question:** ask in [#help on Discord](https://runkora.io/discord).

## Check the docs

The [documentation](https://runkora.dev) is written against the code, and its pages live in [`docs/`](docs/). We'd especially love help with the guides. Follow one against a real install, and when a step is wrong, unclear or missing, open an issue with the page and the step. If you can see the fix, a pull request is just as welcome, and you don't need a running cluster to change the docs.

## Change the code

Get kora running with the [local development guide](docs/contributing/local-development.md). [ARCHITECTURE.md](ARCHITECTURE.md) explains how the services fit together.

Small fixes don't need any discussion first. Reference or open the issue, and include tests. For a new feature or a bigger change, talk it through with us in an issue before you write code. We may ask for a PRD from [docs/prd/template.md](docs/prd/template.md) and an ADR from [docs/adr/template.md](docs/adr/template.md).

Issues labelled [good first issue](https://github.com/runkora/kora/issues?q=state%3Aopen%20label%3A%22good%20first%20issue%22) are a good place to start. If one isn't assigned yet, leave a comment and we'll hand it to you.

### Before you open a pull request

- `make check` passes.
- Tests cover the change.
- Documentation reflects the change.
- The change adds no inline `//nolint` or `eslint-disable` directives.

### Commits and pull requests

- Each pull request branches from `main` and holds one change.
- Commits follow Conventional Commits with an optional scope, such as `fix(policy): fail closed on unbound and unverified delegation tokens`.
- The description references the issue, and the ADR if there is one.
- Commits are signed. `make hooks` runs as part of `make setup` and configures signing with your SSH key and registers the key with GitHub through the `gh` CLI. Without `gh`, add the key on GitHub under **Settings** → **SSH and GPG keys** as a **Signing key**.

Contributions are accepted under the [Apache License 2.0](LICENSE).

## Developer Certificate of Origin

Every commit in a pull request from a fork must be signed off under the [Developer Certificate of Origin](https://developercertificate.org/). A sign-off states that you wrote the change or have the right to submit it under the project's licence. It is a line at the end of the commit message with the commit author's name and email:

```
Signed-off-by: Jane Doe <jane@example.com>
```

`make hooks` installs a hook that adds the line to every commit. Without the hook, commit with `git commit -s`. The DCO check fails a pull request from a fork with any commit that is not signed off. To sign off commits already on your branch, rebase them and push again:

```bash
git rebase --signoff origin/main
git push --force-with-lease
```
