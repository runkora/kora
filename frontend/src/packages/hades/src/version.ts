/**
 * The Hades version, equal to `package.json` `version` and to `manifest.json`
 * `version`. Checked in rather than generated so source consumers and the
 * workspace link see the same value; `make check-versions` fails when the
 * three disagree. See the design system versioning ADR, SD-2.
 */
export const HADES_VERSION = '0.1.0' as const;
