/**
 * Classifies the Hades change at head against the last `hades/v*` release, or
 * against `--base <ref>` while no release exists, and checks the version bump
 * in `package.json` meets the floor the classification sets.
 *
 *   node scripts/classify.ts [--base <ref>] [--format text|markdown|json]
 *
 * Exit 1 when a release exists and the bump is below the floor. Before the
 * first release the report is labelled "no baseline" and nothing is enforced.
 * See the design system versioning ADR, SD-3 and SD-4.
 */
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

import {
  classify,
  compareSemver,
  floorMet,
  formatSemver,
  isDocumentationOnly,
  parseSemver,
  requiredVersion,
  type Classification,
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

// ─── Baseline ────────────────────────────────────────────────────────────────

interface Baseline {
  readonly ref: string;
  readonly label: string;
  readonly version: Semver | null;
  readonly isRelease: boolean;
}

function lastReleaseTag(): { tag: string; version: Semver } | null {
  const tags = (gitOrNull('tag', '--list', 'hades/v*') ?? '')
    .split('\n')
    .map((t) => t.trim())
    .filter(Boolean)
    .flatMap((tag) => {
      const version = parseSemver(tag.slice('hades/v'.length));
      return version ? [{ tag, version }] : [];
    })
    .sort((a, b) => compareSemver(b.version, a.version));
  return tags[0] ?? null;
}

function resolveBaseline(): Baseline {
  const release = lastReleaseTag();
  if (release)
    return { ref: release.tag, label: release.tag, version: release.version, isRelease: true };
  const requested = baseOption ?? 'origin/main';
  const mergeBase = gitOrNull('merge-base', requested, 'HEAD')?.trim();
  const ref = mergeBase || requested;
  const versionText = readAtRef(ref, 'package.json');
  const version = versionText
    ? parseSemver((JSON.parse(versionText) as { version: string }).version)
    : null;
  return {
    ref,
    label: `no baseline (comparing against ${requested}${mergeBase ? ` at ${ref.slice(0, 12)}` : ''})`,
    version,
    isRelease: false,
  };
}

function readAtRef(ref: string, relPath: string): string | null {
  return gitOrNull('show', `${ref}:${packageRel}/${relPath}`);
}

function readAtHead(relPath: string): string | null {
  const full = path.join(packageDir, relPath);
  return fs.existsSync(full) && fs.statSync(full).isFile() ? fs.readFileSync(full, 'utf8') : null;
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

const headPackage = JSON.parse(readAtHead('package.json')!) as {
  version: string;
  peerDependencies?: Record<string, string>;
};
const basePackageText = baseReader('package.json');
const basePackage = basePackageText
  ? (JSON.parse(basePackageText) as { version: string; peerDependencies?: Record<string, string> })
  : null;

const parsedHeadVersion = parseSemver(headPackage.version);
if (!parsedHeadVersion) {
  console.error(`package.json version "${headPackage.version}" is not a semver version`);
  process.exit(1);
}
// Narrowed here so the closures below see a Semver, not Semver | null.
const headVersion: Semver = parsedHeadVersion;

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
  .filter(
    (f) =>
      /\.tsx?$/.test(f) &&
      !isDocumentationOnly(f) &&
      f !== 'src/tokens/surface.ts' &&
      f !== 'src/version.ts',
  );

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

// ─── Floor ───────────────────────────────────────────────────────────────────

const enforced = baseline.isRelease && baseline.version !== null && baseline.version.major >= 1;
const required = baseline.version ? requiredVersion(classification.level, baseline.version) : null;
const met = baseline.version ? floorMet(classification.level, baseline.version, headVersion) : true;
const failed = enforced && !met;

// ─── Report ──────────────────────────────────────────────────────────────────

const REVIEW_QUESTION =
  'Does any token keep its name, kind and format but change what it means, such as an accent repurposed as a status colour? ' +
  'That is a major whatever the table says, and no script decides it.';

function floorLine(level: Level): string {
  if (!baseline.version || !required) return 'no version at the baseline; nothing to enforce';
  const suffix = enforced
    ? met
      ? 'met'
      : 'NOT MET'
    : met
      ? 'met (not enforced before the first release)'
      : 'not met (not enforced before the first release)';
  return `${level} requires at least ${formatSemver(required)}; head is ${formatSemver(headVersion)}: ${suffix}`;
}

function renderText(): string {
  const lines = [
    `Hades classification: ${classification.level}`,
    `Baseline: ${baseline.label}${baseline.version ? ` (version ${formatSemver(baseline.version)})` : ''}`,
    `Head version: ${formatSemver(headVersion)}`,
    `Floor: ${floorLine(classification.level)}`,
    '',
  ];
  if (!baseManifest) lines.push('No manifest at the baseline; manifest rules not applied.', '');
  lines.push(classification.findings.length === 0 ? 'No findings.' : 'Findings:');
  for (const f of classification.findings) {
    lines.push(`  ${f.level.padEnd(5)}  ${f.rule.padEnd(26)}  ${f.subject}: ${f.detail}`);
    for (const m of f.matches ?? []) lines.push(`         ${m}`);
  }
  lines.push('', `Review question: ${REVIEW_QUESTION}`);
  return lines.join('\n');
}

function renderMarkdown(): string {
  const rows = classification.findings.map((f) => {
    const matches = (f.matches ?? []).map((m) => `<br><code>${escapeHtml(m)}</code>`).join('');
    return `| ${f.level} | ${f.rule} | \`${f.subject}\` | ${escapeHtml(f.detail)}${matches} |`;
  });
  return [
    '<!-- hades-classification -->',
    `### Hades classification: **${classification.level}**`,
    '',
    '| | |',
    '|---|---|',
    `| Baseline | ${escapeHtml(baseline.label)} |`,
    `| Version at baseline | ${baseline.version ? formatSemver(baseline.version) : 'none'} |`,
    `| Version at head | ${formatSemver(headVersion)} |`,
    `| Floor | ${escapeHtml(floorLine(classification.level))} |`,
    '',
    ...(baseManifest ? [] : ['No manifest at the baseline; manifest rules not applied.', '']),
    ...(rows.length === 0
      ? ['No findings.']
      : ['| Level | Rule cited | Subject | Detail |', '|---|---|---|---|', ...rows]),
    '',
    `**Review question.** ${REVIEW_QUESTION}`,
  ].join('\n');
}

function escapeHtml(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/\|/g, '\\|');
}

const outputs: Record<string, () => string> = {
  text: renderText,
  markdown: renderMarkdown,
  json: () =>
    JSON.stringify(
      {
        level: classification.level,
        baseline: {
          ref: baseline.ref,
          label: baseline.label,
          version: baseline.version ? formatSemver(baseline.version) : null,
          isRelease: baseline.isRelease,
        },
        headVersion: formatSemver(headVersion),
        required: required ? formatSemver(required) : null,
        floorMet: met,
        enforced,
        findings: classification.findings,
        reviewQuestion: REVIEW_QUESTION,
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
