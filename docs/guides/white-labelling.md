# White-labelling theme

kora lets you customise your install to look and feel like your own brand. A theme sets the product name, colours, logo, favicon and fonts across the dashboard. Agents and tools use the same product name in the text they produce. A deployment has one theme.

The following guide assumes kora is already installed. See [Install runkora on Kubernetes](install-on-kubernetes.md) for the install.

## Write the theme document

```yaml
koraVersion: "1.4.0"
productName: Northwind Agents
tokens:
  --hades-raw-100:
    dark: "#0b1020"
    light: "#f4f6fb"
slots:
  logo:
    dark: https://assets.northwind.example/brand/2026-10/logo-dark.svg
    light: https://assets.northwind.example/brand/2026-10/logo-light.svg
  favicon:
    dark: https://assets.northwind.example/brand/2026-10/favicon-dark.svg
    light: https://assets.northwind.example/brand/2026-10/favicon-light.svg
  font.sans: https://assets.northwind.example/brand/2026-10/inter.woff2
```

- **`koraVersion`:** the release the theme is written against. It is required. Write it as a quoted `MAJOR.MINOR.PATCH`, such as `"1.4.0"`. kora accepts a theme written against its own major version.
- **`productName`:** the name shown in place of kora. It takes up to 40 letters, digits, spaces and `. - & '`.
- **`tokens`:** design tokens, each with a `dark` and a `light` value. The `manifest.json` on each GitHub release lists every token and its format. A mode you leave out uses the kora default.
- **`slots`:** the asset URLs. `logo` and `favicon` take a `dark` and a `light` URL. `font.sans` and `font.mono` take one URL each.

## Host the assets

Each visitor's browser loads the slot URLs directly, including on the login page. Host the files on any HTTPS host your users can reach, such as an object store behind a CDN. Logos and favicons must be `.svg` files. Fonts must be `.woff2` files.

kora validates the form of each URL. It does not fetch the files. After a rollout, check the pages for broken images or fallback fonts.

- Serve fonts with an `Access-Control-Allow-Origin` header for your frontend's origin. Without it, the browser shows the kora font.
- Give a replaced file a new URL. Browsers and CDNs keep serving the old file from an unchanged URL. The dated paths in the example follow this pattern.

## Install the theme

Pass the theme file to the chart with `--set-file`:

```bash
helm upgrade --install kora oci://ghcr.io/runkora/charts/kora \
  --namespace kora-system \
  --values kora.yaml \
  --set-file theme.document=theme.yaml
```

The chart fails to render a document that is not valid YAML or has an invalid `productName`.

A theme change rolls out like any other values change. Revert the change to roll it back.

The frontend validates the theme in a `validate-theme` init container before it starts. An invalid theme stops the new frontend pod. The previous pods keep serving until the theme is fixed. Services that use `productName` roll out with the new name whether or not the frontend accepts the theme.

Read the validation output on the new pod:

```bash
kubectl get pods -n kora-system -l app=kora-frontend
kubectl logs -n kora-system <pod> -c validate-theme
```

The log lists every fault and every entry the release ignores. Each colour pair below its contrast minimum gets a `warning:` line. A theme that sets only some of `productName`, `logo` and `favicon` also gets a warning. Visitors would see a mix of both brands. Warnings do not stop the rollout.

## References

- [White-labelling theme](../adr/2026-10-12-white-labelling-theme.md): how a theme reaches a deployment, and where it is validated.
