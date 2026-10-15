/**
 * Classifies the Hades changes at head against the last final kora release, or
 * against `--base <ref>` while no final release exists, and says what the kora
 * release that carries them must be: none, patch, minor or major.
 *
 *   node scripts/classify.ts [--release <version>] [--base <ref>] [--format text|markdown|json]
 *
 * With `--release`, the version being cut, the script checks that it is at
 * least what the changes require and exits 1 with an explanation when it is
 * not. Without it, as on a pull request, it reports what the next release would
 * owe and always exits 0. Nothing is enforced before the first final release at
 * or above 1.0.0: below it the report is labelled "not enforced". Hades has no
 * version of its own; the release is the version. See the design system
 * versioning ADR, SD-3 and SD-4.
 */
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

import {
  classify,
  floorMet,
  formatSemver,
  isDocumentationOnly,
  lastFinalRelease,
  parseSemver,
  requiredVersion,
  type Classification,
  type Finding,
  type Level,
  type Semver,
} from './lib/classify.ts';
import { readExportSurface, type FileReader } from './lib/exports.ts';
import { parseManifest, type Manifest } from './lib/manifest.ts';

const packageDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');

// Every path given to git is relative to the repository root, so git runs there.
const repoRoot = execFileSync('git', ['rev-parse', '--show-toplevel'], {
  cwd: packageDir,
  encoding: 'utf8',
}).trim();
const packageRel = path.relative(repoRoot, packageDir).split(path.sep).join('/');

function git(...args: string[]): string {
  return execFileSync('git', args, {
    cwd: repoRoot,
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'pipe'],
  });
}

function gitOrNull(...args: string[]): string | null {
  try {
    return git(...args);
  } catch {
    return null;
  }
}

// ─── Arguments ───────────────────────────────────────────────────────────────

const argv = process.argv.slice(2);
function option(name: string): string | undefined {
  const i = argv.indexOf(name);
  return i === -1 ? undefined : argv[i + 1];
}
const format = option('--format') ?? 'text';
const baseOption = option('--base');
const releaseOption = option('--release');

const parsedRelease = releaseOption ? parseSemver(releaseOption) : null;
if (releaseOption && !parsedRelease) {
  console.error(
    `--release "${releaseOption}" is not a version such as 1.4.0, v1.4.0 or 1.4.0-rc.1`,
  );
  process.exit(2);
}
/** The version being cut; null on a pull request, which only reports. */
const release: Semver | null = parsedRelease;

// ─── Baseline ────────────────────────────────────────────────────────────────

interface Baseline {
  readonly ref: string;
  readonly label: string;
  readonly version: Semver | null;
  readonly isRelease: boolean;
}

function isAncestorOfHead(ref: string): boolean {
  return gitOrNull('merge-base', '--is-ancestor', ref, 'HEAD') !== null;
}

function resolveBaseline(): Baseline {
  const tags = (gitOrNull('tag', '--list', 'v*') ?? '')
    .split('\n')
    .map((t) => t.trim())
    .filter(Boolean)
    .filter(isAncestorOfHead);
  const last = lastFinalRelease(tags, release ?? undefined);
  if (last) return { ref: last.tag, label: last.tag, version: last.version, isRelease: true };
  const requested = baseOption ?? 'origin/main';
  const mergeBase = gitOrNull('merge-base', requested, 'HEAD')?.trim();
  return {
    ref: mergeBase || requested,
    label: `no baseline (no final release yet; comparing against ${requested}${mergeBase ? ` at ${mergeBase.slice(0, 12)}` : ''})`,
    version: null,
    isRelease: false,
  };
}

function readAtRef(ref: string, relPath: string): string | null {
  return gitOrNull('show', `${ref}:${packageRel}/${relPath}`);
}

function readAtHead(relPath: string): string | null {
  // Read and let it fail rather than check first: no check-then-use window,
  // and a directory or a missing file both read as absent.
  try {
    return fs.readFileSync(path.join(packageDir, relPath), 'utf8');
  } catch {
    return null;
  }
}

// ─── Inputs ──────────────────────────────────────────────────────────────────

const baseline = resolveBaseline();
const baseReader: FileReader = (rel) => readAtRef(baseline.ref, rel);
const headReader: FileReader = readAtHead;

const headManifestText = readAtHead('manifest.json');
if (!headManifestText) {
  console.error('manifest.json is missing at head. Run: make hades-manifest');
  process.exit(1);
}
const headManifest: Manifest = parseManifest(headManifestText);
const baseManifestText = baseReader('manifest.json');
const baseManifest = baseManifestText ? parseManifest(baseManifestText) : null;

interface PackageJson {
  peerDependencies?: Record<string, string>;
}
const headPackage = JSON.parse(readAtHead('package.json')!) as PackageJson;
const basePackageText = baseReader('package.json');
const basePackage = basePackageText ? (JSON.parse(basePackageText) as PackageJson) : null;

const changedFiles = [
  ...(gitOrNull('diff', '--name-only', baseline.ref, '--', packageRel) ?? '').split('\n'),
  ...(gitOrNull('ls-files', '--others', '--exclude-standard', '--', packageRel) ?? '').split('\n'),
]
  .map((f) => f.trim())
  .filter((f) => f.startsWith(`${packageRel}/`))
  .map((f) => f.slice(packageRel.length + 1))
  .filter((f, i, all) => all.indexOf(f) === i)
  .sort();

/** Component sources present at the baseline, read as they are at head. */
const preExistingSources = (
  gitOrNull('ls-tree', '-r', '--name-only', baseline.ref, '--', `${packageRel}/src`) ?? ''
)
  .split('\n')
  .map((f) => f.trim())
  .filter((f) => f.startsWith(`${packageRel}/src/`))
  .map((f) => f.slice(packageRel.length + 1))
  .filter((f) => /\.tsx?$/.test(f) && !isDocumentationOnly(f) && f !== 'src/tokens/surface.ts');

function findReads(patterns: readonly RegExp[]): string[] {
  const hits: string[] = [];
  for (const file of preExistingSources) {
    const text = readAtHead(file);
    if (text === null) continue;
    text.split('\n').forEach((line, i) => {
      if (patterns.some((p) => p.test(line))) hits.push(`${file}:${i + 1}: ${line.trim()}`);
    });
  }
  return hits;
}

const classification: Classification = classify({
  baseManifest,
  headManifest,
  baseExports: basePackage ? readExportSurface(baseReader) : null,
  headExports: readExportSurface(headReader),
  basePeerDependencies: basePackage?.peerDependencies ?? {},
  headPeerDependencies: headPackage.peerDependencies ?? {},
  changedFiles,
  findReads,
});

// ─── What the release owes ───────────────────────────────────────────────────

const level: Level = classification.package;
const breaksThemes = classification.contract === 'major';
/** Below 1.0.0, and before the first final release, nothing is promised and nothing is blocked. */
const enforced = baseline.isRelease && baseline.version !== null && baseline.version.major >= 1;
const required = baseline.version ? requiredVersion(level, baseline.version) : null;
const met = baseline.version && release ? floorMet(level, baseline.version, release) : true;
const failed = enforced && release !== null && !met;

// ─── Report ──────────────────────────────────────────────────────────────────

const REVIEW_QUESTIONS = [
  'Does any token keep its name, kind and format but change what it means, such as an accent repurposed as a status colour? ' +
    'That breaks themes, so it needs a major, whatever the table says, and no script decides it.',
  'Does any prop narrow its type, or any exported function tighten its signature? ' +
    'That breaks consumers of the package, so it needs a major, and the classifier reads member names and optionality, not types.',
];

const LEVEL_MEANING: Record<Level, string> = {
  major:
    'a major release. A major means a theme written for an earlier release can fail validation or change meaning, or a consumer of @runkora/hades can fail to compile or render.',
  minor: 'a minor release or higher. A minor means the surface or the package gained something.',
  patch: 'a new version above the last final release.',
  none: 'no new version.',
};

function releaseText(): string {
  return release ? formatSemver(release) : 'not given (pull request report)';
}

function enforcementText(): string {
  if (!baseline.version) return 'no final release to compare with; nothing to enforce';
  if (!required) return 'nothing to enforce';
  const lead = `${level} requires at least ${formatSemver(required)}`;
  if (!release) return `${lead}; the next release must be that or higher`;
  const state = met ? 'met' : 'NOT MET';
  return enforced
    ? `${lead}; this release is ${formatSemver(release)}: ${state}`
    : `${lead}; this release is ${formatSemver(release)}: ${met ? 'met' : 'not met'} (not enforced below 1.0.0)`;
}

/** The fixed explanation printed when a release is blocked. */
function blockedLines(esc: (s: string) => string): string[] {
  const base = formatSemver(baseline.version!);
  const rel = formatSemver(release!);
  const need = formatSemver(required!);
  const causes = classification.findings.filter((f) => f.package === level);
  const isBreak = level === 'major';
  const lines = [
    `HADES RELEASE BLOCKED: v${rel} is too low for the Hades changes in it.`,
    '',
    `This release is v${rel}. The last final release is v${base}. Between them Hades changed in a way that needs ${LEVEL_MEANING[level]} ` +
      `So this release must be at least v${need}.`,
    '',
    'Why this is checked: Hades is released with kora under one version, so the kora version has to say what Hades did. ' +
      (isBreak
        ? breaksThemes
          ? 'At least one change below breaks themes, so every deployed theme would be affected by a release that claims otherwise.'
          : 'No change below breaks themes, but each breaks consumers of the published @runkora/hades package, which follow caret ranges.'
        : 'A package consumer on a caret range relies on additions arriving as a minor at least.'),
    '',
    `What changed (${causes.length} finding${causes.length === 1 ? '' : 's'} at ${level}):`,
    ...causes.flatMap((f) => [
      `  - ${esc(f.subject)}: ${esc(f.detail)} [${f.rule}]`,
      ...(f.matches ?? []).map((m) => `      ${esc(m)}`),
    ]),
    '',
    'How to fix it:',
    `  1. Release as v${need} or higher. Delete this tag if it still exists (git push origin --delete v${rel}; git tag -d v${rel}), then tag the same commit as v${need}.`,
    ...(isBreak
      ? [
          `  2. Or keep v${rel}: revert or rework the breaking changes listed above so Hades does not break, then re-cut the release.`,
        ]
      : []),
    '',
    `Reproduce locally: make hades-classify RELEASE=${rel}`,
    '',
    'This check cannot see two cases, so a release can owe a major it does not report:',
    ...REVIEW_QUESTIONS.map((q) => `  - ${q}`),
  ];
  return lines;
}

function renderText(): string {
  const identity = (s: string): string => s;
  const lines: string[] = failed ? [...blockedLines(identity), '', '---', ''] : [];
  lines.push(
    `Hades classification: ${level}${breaksThemes ? ' (breaks themes)' : ''}`,
    `Baseline: ${baseline.label}${baseline.version ? ` (version ${formatSemver(baseline.version)})` : ''}`,
    `Release: ${releaseText()}`,
    `Owes:     ${enforcementText()}`,
    '',
  );
  if (!baseManifest) lines.push('No manifest at the baseline; manifest rules not applied.', '');
  lines.push(
    classification.findings.length === 0 ? 'No findings.' : 'Findings (breaks themes / level):',
  );
  for (const f of classification.findings) {
    lines.push(
      `  ${(f.contract === 'major' ? 'yes' : 'no').padEnd(3)} / ${f.package.padEnd(5)}  ${f.rule.padEnd(26)}  ${f.subject}: ${f.detail}`,
    );
    for (const m of f.matches ?? []) lines.push(`         ${m}`);
  }
  for (const q of REVIEW_QUESTIONS) lines.push('', `Review question: ${q}`);
  return lines.join('\n');
}

function renderMarkdown(): string {
  const row = (f: Finding): string => {
    const matches = (f.matches ?? []).map((m) => `<br><code>${escapeHtml(m)}</code>`).join('');
    return `| ${f.package} | ${f.contract === 'major' ? 'yes' : 'no'} | ${f.rule} | \`${f.subject}\` | ${escapeHtml(f.detail)}${matches} |`;
  };
  return [
    ...(failed
      ? ['### Hades release blocked', '', '```text', ...blockedLines((s) => s), '```', '']
      : []),
    `### Hades classification: **${level}**${breaksThemes ? ', breaks themes' : ''}`,
    '',
    '| | |',
    '|---|---|',
    `| Baseline | ${escapeHtml(baseline.label)} |`,
    `| Release | ${escapeHtml(releaseText())} |`,
    `| Owes | ${escapeHtml(enforcementText())} |`,
    '',
    ...(baseManifest ? [] : ['No manifest at the baseline; manifest rules not applied.', '']),
    ...(classification.findings.length === 0
      ? ['No findings.']
      : [
          '| Level | Breaks themes | Rule cited | Subject | Detail |',
          '|---|---|---|---|---|',
          ...classification.findings.map(row),
        ]),
    '',
    ...REVIEW_QUESTIONS.map((q) => `**Review question.** ${q}`).flatMap((q) => [q, '']),
  ].join('\n');
}

function escapeHtml(s: string): string {
  return s
    .replace(/\\/g, '\\\\')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/\|/g, '\\|');
}

const outputs: Record<string, () => string> = {
  text: renderText,
  markdown: renderMarkdown,
  json: () =>
    JSON.stringify(
      {
        level,
        breaksThemes,
        baseline: {
          ref: baseline.ref,
          label: baseline.label,
          version: baseline.version ? formatSemver(baseline.version) : null,
          isRelease: baseline.isRelease,
        },
        release: release ? formatSemver(release) : null,
        requiredVersion: required ? formatSemver(required) : null,
        met,
        enforced,
        blocked: failed,
        findings: classification.findings,
        reviewQuestions: REVIEW_QUESTIONS,
      },
      null,
      2,
    ),
};

const render = outputs[format];
if (!render) {
  console.error(`unknown --format ${format}; use text, markdown or json`);
  process.exit(2);
}
console.log(render());
process.exit(failed ? 1 : 0);
