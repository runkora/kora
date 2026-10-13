/**
 * The Hades version, equal to `package.json` `version` and to `manifest.json`
 * `version`. Checked in rather than generated so source consumers and the
 * workspace link see the same value; `make check-versions` fails when the
 * three disagree. See the design system versioning ADR, SD-2.
 */
export const HADES_VERSION = '1.0.0' as const;

/**
 * The theme contract version, `MAJOR.MINOR`, equal to `manifest.json`
 * `contractVersion`. A theme targets this, not `HADES_VERSION`: it moves only
 * when the surface a theme sets moves, which the classifier decides, and
 * independently of the package version. A consumer that renders themes reads
 * it to decide whether a theme is compatible. Starts at `1.0` with Hades
 * `1.0.0`, the first release that carries a contract. See the design system
 * versioning ADR, SD-3.
 */
export const CONTRACT_VERSION = '1.0' as const;
