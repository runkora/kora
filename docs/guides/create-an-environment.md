# Create an environment

In runkora, an environment defines the [sandbox](../concepts/sandboxes.md) an agent runs its commands, builds and tests in. Each environment has a sandbox image, a size, and the egress hosts and connections agents get with it. Platform teams register the images, then create environments for agents to use. Creating, editing and deleting environments needs `agents:environments:write`. Of the built-in roles, only `platform-admin` holds it.

## Register a sandbox image

kora registers the `kora-base` image in every tenant, with command-line tools, Go and Python. It also registers three bundled images built on it: `kora-go`, `kora-python` and `kora-node`. To give agents your own toolchain, build an image `FROM kora-sandbox-base` and register it.

```dockerfile
FROM ghcr.io/runkora/kora-sandbox-base:<version>
USER 0
RUN apt-get update && apt-get install -y --no-install-recommends postgresql-client \
    && rm -rf /var/lib/apt/lists/*
USER 100:100
```

Install tools into system paths such as `/usr/local/bin`. At run time a sandbox uses its local disk as `HOME`. Files baked into `/home/kora` are not available there. Do not override the base image's command, the server kora sends commands to.

Register the image as a `SandboxImage` in the tenant namespace:

```yaml
apiVersion: runkora.io/v1alpha1
kind: SandboxImage
metadata:
  name: custom-team-go
  namespace: kora-tenant-kora
spec:
  image: ghcr.io/kora/sandbox-go:1.4.0
  description: Go with buf and golangci-lint.
  warm: true
  pullSecretRef:
    name: kora-registry
```

The kora operator reads the image from the registry and checks it before any environment can use it:

| Check | Fails with |
| --- | --- |
| The image runs as `USER 100` or `100:<gid>`. A user name such as `kora` is rejected. | `WrongUser` |
| The image carries the `io.runkora.sandbox.base` label, which the base image sets | `MissingBaseLabel` |
| The registry can be read, with `pullSecretRef` when the image is private | `RegistryError` or `PullSecretError` |

Every sandbox pulls a valid image by its pinned digest. Check an image's state with `kubectl -n kora-tenant-kora get sandboximages`. Setting `warm: true` keeps the image pulled on every sandbox node. New sandboxes then start without waiting for a download.

## Create the environment

Open **Environments** and choose **New environment**. Start from a registered image, or copy an existing environment and change what you need.

- **Name and description:** agents read the description when they choose an environment. Mention the repositories or tasks it is for. The slug is fixed once the environment is created.
- **Image:** choose a valid sandbox image. Images marked **Warm** are already pulled on the sandbox nodes and start fastest.
- **Size:** choose Small, Medium or Large, or set CPU, memory and local disk yourself. Medium is the platform default. Large is the most a sandbox node can hold. The local disk holds the sandbox's home, caches and installed tools.
- **Access:** add the hosts agents can reach from this environment, such as `api.github.com` or `*.internal.kora.com`. **Add common package registries** adds npm, PyPI, the Go proxy, crates.io and Debian. Grant actions on [connections](register-connection.md) the same way as on an agent.

The effective access panel shows everything agents can reach with the environment.

## Attach it to an agent

Attach environments when you [create an agent](create-an-agent.md), or from the agent's configuration page. The first environment you attach is the agent's default. **Make default** changes it. An agent with no environment runs on `kora-base` at the default size.

An agent can use the hosts and connections of every environment attached to it, whichever environment its sandbox runs in. At run time, the agent lists its environments with `list_environments`, starts a sandbox in one with `provision_sandbox`, and moves to another with `replace_environment`. Replacing an environment deletes the agent's sandbox and starts a new one in the other environment. Everything in the old sandbox is gone, including running commands, build caches and installed tools. The run's workspace is not part of the sandbox. Its files carry over.

An environment attached to an agent cannot be deleted. Detach it from every agent first.

Environments can also be managed through the API at `/api/v1/environments` and the `agents_environments_*` tools in the kora MCP server. Sandbox images are registered only as `SandboxImage` resources, with `kubectl` or GitOps.
