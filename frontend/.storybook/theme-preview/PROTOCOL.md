# Theme-preview protocol, version 1

The built Storybook ships as a kora release asset, `kora-storybook-vX.Y.Z.zip`. Every story under the `Pages/` prefix renders the real page. A page can be previewed in a theme by sending the Storybook iframe a theme over `postMessage`. See the white-labelling theme ADR, Decision 6.

## What to send

Serve the unzipped archive, embed a story (`iframe.html?id=<story-id>&viewMode=story`) in an iframe, and post to the iframe's own `contentWindow`:

```js
iframe.contentWindow.postMessage(
  {
    type: 'kora-theme-preview',
    version: 1,
    document: themeYamlText, // the text of theme.yaml
    assets: {
      // bundle-relative path -> a URL you serve the file from
      'fonts/Brand.woff2': 'https://example.test/bundle/fonts/Brand.woff2',
      'logo.svg': 'https://example.test/bundle/logo.svg',
    },
  },
  '*', // or the Storybook origin
);
```

To drop the theme and show the kora default again: `{ type: 'kora-theme-preview', version: 1, clear: true }`.

Same-origin is not required. You must target the iframe's window, and listen on yours for the reply. Adding `?theme-preview=1` to the iframe URL is allowed and has no effect.

## What comes back

The iframe replies to the sender (`event.source`) with:

```ts
{
  type: 'kora-theme-preview-result',
  version: 1,
  ok: boolean,
  errors?: { path: string; message: string }[], // when ok is false; same faults the deploy would report
  report?: {
    ignored: { kind: 'token' | 'slot'; name: string }[], // entries the pinned Hades does not know
    fallThrough: string[], // identity values left to the kora default
  },
}
```

On `ok: true` the generated CSS is injected as `<style data-theme-preview>` in the preview document's head, replacing any previous one. On `ok: false` the previous theme stays. A `clear` message is answered with `ok: true`.

A message whose `version` is not `1` is rejected with `ok: false` and an error at path `version`; nothing is injected. A message of type `kora-theme-preview` with a malformed body is answered the same way, and a message of any other type is ignored. The listener never throws.

The theme is validated with the Hades validator at the version in `preview.json`, so the preview and a real deploy accept and reject the same documents. Each asset is fetched from the URL you supplied; a path the document references that is missing from `assets` is reported as a missing file.

## Discovery

To find the pages for Hades major N, walk kora releases newest first, read the `hades:` line of each release's `compatibility.yaml`, and take the first whose major is N. The archive's own `preview.json` (`{ "kora", "hades", "protocol" }`) records the same versions and the protocol version, so a downloaded archive is self-describing.
