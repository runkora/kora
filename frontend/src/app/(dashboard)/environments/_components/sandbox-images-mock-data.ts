import type { SandboxImage } from '@orbit/shared/api';

/** A digest-like string derived from the name, so every mock image has a stable digest. */
function fakeDigest(seed: string): string {
  let h = 2166136261;
  let out = '';
  for (let round = 0; out.length < 64; round++) {
    for (const ch of `${seed}${round}`) h = Math.imul(h ^ ch.charCodeAt(0), 16777619) >>> 0;
    out += h.toString(16).padStart(8, '0');
  }
  return `sha256:${out.slice(0, 64)}`;
}

type Spec = [name: string, description: string, ref: string, warm: boolean, problem?: string];

const SPECS: Spec[] = [
  ['kora-base', 'The kora base image.', 'ghcr.io/clear-route/kora-sandbox-base:1.0.0', true],
  ['acme-go', 'Go 1.26, buf and golangci-lint.', 'registry.acme.example/sandbox/go:1.26', true],
  [
    'acme-go-legacy',
    'Go 1.22 for the billing monolith.',
    'registry.acme.example/sandbox/go:1.22',
    false,
  ],
  [
    'acme-go-cgo',
    'Go 1.26 with gcc and musl for cgo builds.',
    'registry.acme.example/sandbox/go-cgo:1.26',
    false,
  ],
  ['acme-node', 'Node 22 and pnpm.', 'registry.acme.example/sandbox/node:22', true],
  [
    'acme-node-20',
    'Node 20 LTS and npm for older services.',
    'registry.acme.example/sandbox/node:20',
    false,
  ],
  [
    'acme-node-browsers',
    'Node 22 with Chromium and Playwright for UI tests.',
    'registry.acme.example/sandbox/node-browsers:22',
    true,
  ],
  ['acme-bun', 'Bun 1.2 for the edge workers.', 'registry.acme.example/sandbox/bun:1.2', false],
  ['acme-python', 'Python 3.13, uv and ruff.', 'registry.acme.example/sandbox/python:3.13', true],
  [
    'acme-python-311',
    'Python 3.11 for the reporting jobs.',
    'registry.acme.example/sandbox/python:3.11',
    false,
  ],
  [
    'acme-python-geo',
    'Python 3.13 with GDAL and PROJ.',
    'registry.acme.example/sandbox/python-geo:3.13',
    false,
    'image is not built on the kora base image',
  ],
  [
    'acme-rust',
    'Rust stable, cargo-nextest and clippy.',
    'registry.acme.example/sandbox/rust:1.90',
    true,
  ],
  [
    'acme-rust-nightly',
    'Rust nightly for the parser crates.',
    'registry.acme.example/sandbox/rust:nightly',
    false,
  ],
  ['acme-java', 'Temurin 21, Maven and Gradle.', 'registry.acme.example/sandbox/java:21', false],
  [
    'acme-java-17',
    'Temurin 17 for the payments service.',
    'registry.acme.example/sandbox/java:17',
    false,
    'image runs as uid 0, want uid 100',
  ],
  [
    'acme-kotlin',
    'Kotlin 2.2 and Gradle with a warm cache.',
    'registry.acme.example/sandbox/kotlin:2.2',
    false,
  ],
  ['acme-dotnet', '.NET 9 SDK.', 'registry.acme.example/sandbox/dotnet:9', false],
  ['acme-ruby', 'Ruby 3.4 and Bundler.', 'registry.acme.example/sandbox/ruby:3.4', false],
  [
    'acme-php',
    'PHP 8.4 and Composer.',
    'registry.acme.example/sandbox/php:8.4',
    false,
    'image has no digest; pin it by digest',
  ],
  [
    'acme-data',
    'dbt, DuckDB and the Snowflake CLI.',
    'registry.acme.example/sandbox/data:2026.09',
    true,
  ],
  [
    'acme-data-spark',
    'PySpark 4 and Delta Lake for batch jobs.',
    'registry.acme.example/sandbox/data-spark:4.0',
    false,
  ],
  [
    'acme-ml',
    'PyTorch 2.8 (CPU), numpy and scikit-learn.',
    'registry.acme.example/sandbox/ml:2.8-cpu',
    false,
  ],
  [
    'acme-ml-cuda',
    'PyTorch 2.8 with CUDA 12 for GPU nodes.',
    'registry.acme.example/sandbox/ml:2.8-cuda12',
    false,
    'image is 18Gi, over the 10Gi limit',
  ],
  [
    'acme-ml-notebooks',
    'Jupyter, pandas and matplotlib for analysis runs.',
    'registry.acme.example/sandbox/ml-notebooks:2026.09',
    false,
  ],
  [
    'acme-ops',
    'kubectl and helm for incident work.',
    'registry.acme.example/sandbox/ops:3',
    false,
    'image runs as uid 0, want uid 100',
  ],
  [
    'acme-ops-terraform',
    'Terraform 1.13, tflint and the AWS CLI.',
    'registry.acme.example/sandbox/ops-terraform:1.13',
    true,
  ],
  [
    'acme-ops-ansible',
    'Ansible 11 and the vault CLI.',
    'registry.acme.example/sandbox/ops-ansible:11',
    false,
  ],
  [
    'acme-security',
    'Trivy, Semgrep and gitleaks for scanning.',
    'registry.acme.example/sandbox/security:2026.09',
    false,
  ],
  [
    'acme-docs',
    'Hugo, Vale and markdownlint for the docs site.',
    'registry.acme.example/sandbox/docs:2026.09',
    false,
  ],
  ['acme-mobile', 'Android SDK 35 and Fastlane.', 'registry.acme.example/sandbox/mobile:35', false],
];

const MANY_IMAGES: SandboxImage[] = SPECS.map(([name, description, image, warm, problem]) => ({
  name,
  description,
  image,
  digest: problem?.includes('digest') ? undefined : fakeDigest(name),
  valid: !problem,
  problem,
  warm,
}));

const TEAMS = [
  'payments',
  'search',
  'growth',
  'platform',
  'identity',
  'billing',
  'mobile',
  'data-eng',
  'risk',
];
const STACKS: [suffix: string, description: string, ref: string][] = [
  ['go', 'Go 1.26 with the team linters', 'go:1.26'],
  ['node', 'Node 22, pnpm and the team test runner', 'node:22'],
  ['python', 'Python 3.13 and uv with the team wheels cached', 'python:3.13'],
  ['rust', 'Rust stable with sccache', 'rust:1.90'],
  ['java', 'Temurin 21 and Gradle with a warm cache', 'java:21'],
  ['data', 'dbt and DuckDB with the team models', 'data:2026.09'],
  ['ops', 'kubectl, helm and the team runbooks', 'ops:3'],
  ['ml', 'PyTorch 2.8 (CPU) with the team feature store client', 'ml:2.8-cpu'],
  ['e2e', 'Node 22 with Chromium for end-to-end tests', 'e2e:22'],
  ['docs', 'Hugo and Vale with the team style guide', 'docs:2026.09'],
];
const TEAM_PROBLEMS = [
  'image runs as uid 0, want uid 100',
  'image is not built on the kora base image',
  'image is 14Gi, over the 10Gi limit',
];

/** 120 images for the wizard stories: the 30 above plus 90 team-scoped variants, a few of them invalid. */
export const MANY_IMAGES_120: SandboxImage[] = [
  ...MANY_IMAGES,
  ...TEAMS.flatMap((team, t) =>
    STACKS.map(([suffix, description, ref], k): SandboxImage => {
      const n = t * STACKS.length + k;
      const problem = n % 13 === 5 ? TEAM_PROBLEMS[n % TEAM_PROBLEMS.length] : undefined;
      const name = `${team}-${suffix}`;
      return {
        name,
        description: `${description} for ${team}.`,
        image: `registry.acme.example/teams/${team}/sandbox-${ref}`,
        digest: fakeDigest(name),
        valid: !problem,
        problem,
        warm: n % 7 === 0,
      };
    }),
  ),
];
