/**
 * Classifies the Hades change at head against the last `hades/v*` release, or
 * against `--base <ref>` while no release exists, on two axes, and checks the
 * version bumps meet the two floors the classification sets: `package.json`
 * `version` for the package, and `manifest.json` `contractVersion` for the
 * theme contract.
 *
 *   node scripts/classify.ts [--base <ref>] [--format text|markdown|json]
 *
 * Exit 1 when a release exists and either bump is below its floor. Before the
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
  contractFloorMet,
  floorMet,
  formatContractVersion,
  formatSemver,
  isDocumentationOnly,
  parseContractVersion,
  parseSemver,
  requiredContractVersion,
  requiredVersion,
  type Classification,
  type ContractLevel,
  type ContractVersion,
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

const parsedHeadContract = parseContractVersion(headManifest.contractVersion);
if (!parsedHeadContract) {
  console.error(
    `manifest.json contractVersion "${headManifest.contractVersion}" is not MAJOR.MINOR. Run: make hades-manifest`,
  );
  process.exit(1);
}
const headContract: ContractVersion = parsedHeadContract;
const baseContract: ContractVersion | null = baseManifest?.contractVersion
  ? parseContractVersion(baseManifest.contractVersion)
  : null;

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

// ─── Floors ──────────────────────────────────────────────────────────────────

const enforced = baseline.isRelease && baseline.version !== null && baseline.version.major >= 1;
const required = baseline.version
  ? requiredVersion(classification.package, baseline.version)
  : null;
const met = baseline.version
  ? floorMet(classification.package, baseline.version, headVersion)
  : true;
const requiredContract = baseContract
  ? requiredContractVersion(classification.contract, baseContract)
  : null;
const contractMet = baseContract
  ? contractFloorMet(classification.contract, baseContract, headContract)
  : true;
const failed = enforced && (!met || !contractMet);

// ─── Report ──────────────────────────────────────────────────────────────────

const REVIEW_QUESTIONS = [
  'Does any token keep its name, kind and format but change what it means, such as an accent repurposed as a status colour? ' +
    'That is a contract major whatever the table says, and no script decides it.',
  'Does any prop narrow its type, or any exported function tighten its signature? ' +
    'That is a package major, and the classifier reads member names and optionality, not types.',
];

function status(isMet: boolean): string {
  return enforced
    ? isMet
      ? 'met'
      : 'NOT MET'
    : isMet
      ? 'met (not enforced before the first release)'
      : 'not met (not enforced before the first release)';
}

function packageFloorLine(level: Level): string {
  if (!baseline.version || !required) return 'no version at the baseline; nothing to enforce';
  return `${level} requires at least ${formatSemver(required)}; head is ${formatSemver(headVersion)}: ${status(met)}`;
}

function contractFloorLine(level: ContractLevel): string {
  if (!baseContract || !requiredContract)
    return 'no contract version at the baseline; nothing to enforce';
  return `${level} requires at least ${formatContractVersion(requiredContract)}; head is ${formatContractVersion(headContract)}: ${status(contractMet)}`;
}

function renderText(): string {
  const lines = [
    `Hades classification: package ${classification.package}, contract ${classification.contract}`,
    `Baseline: ${baseline.label}${baseline.version ? ` (version ${formatSemver(baseline.version)}${baseContract ? `, contract ${formatContractVersion(baseContract)}` : ''})` : ''}`,
    `Head: version ${formatSemver(headVersion)}, contract ${formatContractVersion(headContract)}`,
    `Package floor:  ${packageFloorLine(classification.package)}`,
    `Contract floor: ${contractFloorLine(classification.contract)}`,
    '',
  ];
  if (!baseManifest) lines.push('No manifest at the baseline; manifest rules not applied.', '');
  lines.push(
    classification.findings.length === 0 ? 'No findings.' : 'Findings (contract / package):',
  );
  for (const f of classification.findings) {
    lines.push(
      `  ${f.contract.padEnd(5)} / ${f.package.padEnd(5)}  ${f.rule.padEnd(26)}  ${f.subject}: ${f.detail}`,
    );
    for (const m of f.matches ?? []) lines.push(`         ${m}`);
  }
  for (const q of REVIEW_QUESTIONS) lines.push('', `Review question: ${q}`);
  return lines.join('\n');
}

function renderMarkdown(): string {
  const rows = classification.findings.map((f) => {
    const matches = (f.matches ?? []).map((m) => `<br><code>${escapeHtml(m)}</code>`).join('');
    return `| ${f.contract} | ${f.package} | ${f.rule} | \`${f.subject}\` | ${escapeHtml(f.detail)}${matches} |`;
  });
  return [
    '<!-- hades-classification -->',
    `### Hades classification: package **${classification.package}**, contract **${classification.contract}**`,
    '',
    '| | Package | Contract |',
    '|---|---|---|',
    `| Baseline | ${escapeHtml(baseline.label)} | |`,
    `| At baseline | ${baseline.version ? formatSemver(baseline.version) : 'none'} | ${baseContract ? formatContractVersion(baseContract) : 'none'} |`,
    `| At head | ${formatSemver(headVersion)} | ${formatContractVersion(headContract)} |`,
    `| Floor | ${escapeHtml(packageFloorLine(classification.package))} | ${escapeHtml(contractFloorLine(classification.contract))} |`,
    '',
    ...(baseManifest ? [] : ['No manifest at the baseline; manifest rules not applied.', '']),
    ...(rows.length === 0
      ? ['No findings.']
      : [
          '| Contract | Package | Rule cited | Subject | Detail |',
          '|---|---|---|---|---|',
          ...rows,
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
        package: classification.package,
        contract: classification.contract,
        baseline: {
          ref: baseline.ref,
          label: baseline.label,
          version: baseline.version ? formatSemver(baseline.version) : null,
          contractVersion: baseContract ? formatContractVersion(baseContract) : null,
          isRelease: baseline.isRelease,
        },
        head: {
          version: formatSemver(headVersion),
          contractVersion: formatContractVersion(headContract),
        },
        required: {
          version: required ? formatSemver(required) : null,
          contractVersion: requiredContract ? formatContractVersion(requiredContract) : null,
        },
        floorMet: { package: met, contract: contractMet },
        enforced,
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
