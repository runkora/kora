# Hades theme contract boundary rules

The theme contract is the Hades major. These five rules decide what is and is not a change to it, and every classification the `scripts/classify.ts` classifier reports cites the rule it applied. The same list is in the Hades Design System PRD and in subsection 3 of the design system versioning ADR; `make hades-contract-check` fails when the three copies differ.

1. Hades defines the shape of every value a theme can set. That shape is the theme surface manifest: colour and shadow tokens per mode, radius steps, font slots, identity slots (the logo and the favicon, each per mode) and the product name with its bounds.
2. Who reads a value is not the contract's concern. The kora frontend and the kora backend both read the product name, and neither reading is part of the contract.
3. Hades contains no kora identity. The kora default is a theme like any other, owned by the app. The kora logo is an asset of that theme, not a Hades component. Hades provides the agnostic components that render whatever a theme supplies for a slot.
4. A kora change that adds a place where a value appears, or changes a page or a template, does not change the contract. A kora change that needs a new value changes the manifest, which is a Hades change and is classified as one.
5. kora default values are outside the contract. A change to a default value alone is a patch.

The surface itself is `manifest.json`, generated from `src/tokens/tokens.css` and `src/tokens/surface.ts` by `make hades-manifest`.
