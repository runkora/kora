/**
 * Marketplace sample data for e2e route mocks.
 *
 * The skills-service is a live Go backend, so Category A
 * (docs/adr/2026-05-08-frontend-test-strategy-and-toolchain.md) forbids
 * marketplace.spec.ts from reaching it. These fixtures stand in for its
 * responses behind page.route() on the two BFF endpoints the browser calls:
 * /api/marketplace/skills and /api/marketplace/skill. The slugs follow the
 * backend rules: marketplaceSlug = org__repo, skillSlug =
 * marketplaceSlug__slugify(name).
 */
import type { Marketplace, Skill, SkillSummary } from '@orbit/shared/api';

export const MARKETPLACE_FIXTURES: Marketplace[] = [
  {
    name: 'ClearRoute Core',
    marketplaceSlug: 'clearroute__core-skills',
    link: 'https://github.com/clearroute/core-skills',
  },
  {
    name: 'ClearRoute Platform',
    marketplaceSlug: 'clearroute__platform-skills',
    link: 'https://github.com/clearroute/platform-skills',
  },
  {
    name: 'Partner Reference',
    marketplaceSlug: 'partner-org__reference-skills',
    link: 'https://github.com/partner-org/reference-skills',
  },
  {
    name: 'Platform Engineering',
    marketplaceSlug: 'platform-engineering__skills',
    link: 'https://github.com/platform-engineering/skills',
  },
];

/** The Skill list of each Marketplace, keyed by marketplaceSlug. */
export const SKILL_FIXTURES_BY_MARKETPLACE: Record<string, SkillSummary[]> = {
  'clearroute__core-skills': [
    {
      name: 'Incident Triage',
      description: 'Triages incoming incidents by severity.',
      skillSlug: 'clearroute__core-skills__incident-triage',
    },
    {
      name: 'Change Freeze Checker',
      description: 'Reports whether a change window is currently open.',
      skillSlug: 'clearroute__core-skills__change-freeze-checker',
    },
    {
      name: 'Customer Impact Summariser',
      description: 'Estimates affected customers from an incident.',
      skillSlug: 'clearroute__core-skills__customer-impact-summariser',
    },
    {
      name: 'Escalation Router',
      description: 'Routes an incident to the correct on-call rota.',
      skillSlug: 'clearroute__core-skills__escalation-router',
    },
    {
      name: 'Postmortem Reviewer',
      description: 'Checks a postmortem for action-item quality.',
      skillSlug: 'clearroute__core-skills__postmortem-reviewer',
    },
    {
      name: 'Status Page Drafter',
      description: 'Drafts a customer-facing status update.',
      skillSlug: 'clearroute__core-skills__status-page-drafter',
    },
  ],
  'clearroute__platform-skills': [
    {
      name: 'Incident Triage',
      description: 'Triages incoming incidents by owning team.',
      skillSlug: 'clearroute__platform-skills__incident-triage',
    },
    {
      name: 'Cost Report',
      description: 'Summarises spend by environment.',
      skillSlug: 'clearroute__platform-skills__cost-report',
    },
  ],
  'partner-org__reference-skills': [],
  'platform-engineering__skills': [
    {
      name: 'React Expert',
      description: 'Senior React developer.',
      skillSlug: 'platform-engineering__skills__react-expert',
    },
    {
      name: 'React Reviewer',
      description: 'Reviews React components for accessibility and state handling.',
      skillSlug: 'platform-engineering__skills__react-reviewer',
    },
    {
      name: 'Airflow Access Review',
      description: 'List who can reach Airflow and flag grants with no named owner.',
      skillSlug: 'platform-engineering__skills__airflow-access-review',
    },
    {
      name: 'Alert Noise Reducer',
      description: 'Groups duplicate alerts and proposes threshold changes.',
      skillSlug: 'platform-engineering__skills__alert-noise-reducer',
    },
    {
      name: 'Ansible Drift Check',
      description: 'Compares live host state against playbook intent.',
      skillSlug: 'platform-engineering__skills__ansible-drift-check',
    },
    {
      name: 'API Contract Diff',
      description: 'Diffs OpenAPI specifications and flags breaking changes.',
      skillSlug: 'platform-engineering__skills__api-contract-diff',
    },
    {
      name: 'Argo Sync Auditor',
      description: 'Reports applications that are out of sync with Git.',
      skillSlug: 'platform-engineering__skills__argo-sync-auditor',
    },
    {
      name: 'Backup Restore Drill',
      description: 'Walks a restore rehearsal and records the outcome.',
      skillSlug: 'platform-engineering__skills__backup-restore-drill',
    },
    {
      name: 'Bazel Cache Analyst',
      description: 'Explains cache misses in a build graph.',
      skillSlug: 'platform-engineering__skills__bazel-cache-analyst',
    },
    {
      name: 'Bicep Linter',
      description: 'Reviews Azure Bicep templates for policy violations.',
      skillSlug: 'platform-engineering__skills__bicep-linter',
    },
    {
      name: 'Budget Forecaster',
      description: 'Projects month-end cloud spend from the current burn rate.',
      skillSlug: 'platform-engineering__skills__budget-forecaster',
    },
    {
      name: 'Cert Expiry Sweep',
      description: 'Lists TLS certificates expiring within thirty days.',
      skillSlug: 'platform-engineering__skills__cert-expiry-sweep',
    },
    {
      name: 'CI Flake Hunter',
      description: 'Finds tests that fail intermittently across runs.',
      skillSlug: 'platform-engineering__skills__ci-flake-hunter',
    },
    {
      name: 'Cluster Upgrade Planner',
      description: 'Sequences a Kubernetes version upgrade.',
      skillSlug: 'platform-engineering__skills__cluster-upgrade-planner',
    },
    {
      name: 'Cost Anomaly Explainer',
      description: 'Attributes a spend spike to a specific service change.',
      skillSlug: 'platform-engineering__skills__cost-anomaly-explainer',
    },
    {
      name: 'Dashboard Gap Finder',
      description: 'Flags services with no latency dashboard.',
      skillSlug: 'platform-engineering__skills__dashboard-gap-finder',
    },
    {
      name: 'Data Retention Auditor',
      description: 'Checks table retention settings against policy.',
      skillSlug: 'platform-engineering__skills__data-retention-auditor',
    },
    {
      name: 'DNS Record Reviewer',
      description: 'Finds dangling and duplicate DNS records.',
      skillSlug: 'platform-engineering__skills__dns-record-reviewer',
    },
    {
      name: 'Docker Image Slimmer',
      description: 'Proposes smaller base images and merged layers.',
      skillSlug: 'platform-engineering__skills__docker-image-slimmer',
    },
    {
      name: 'Envoy Config Explainer',
      description: 'Explains an Envoy listener chain in plain terms.',
      skillSlug: 'platform-engineering__skills__envoy-config-explainer',
    },
    {
      name: 'Feature Flag Reaper',
      description: 'Lists flags fully rolled out and safe to remove.',
      skillSlug: 'platform-engineering__skills__feature-flag-reaper',
    },
    {
      name: 'GitHub Action Auditor',
      description: 'Reviews workflow permissions and unpinned actions.',
      skillSlug: 'platform-engineering__skills__github-action-auditor',
    },
    {
      name: 'Grafana Panel Builder',
      description: "Drafts dashboard panels from a service's metric names.",
      skillSlug: 'platform-engineering__skills__grafana-panel-builder',
    },
    {
      name: 'Helm Values Differ',
      description: 'Diffs rendered Helm output between two releases.',
      skillSlug: 'platform-engineering__skills__helm-values-differ',
    },
    {
      name: 'IAM Policy Minimiser',
      description: 'Narrows over-broad IAM policies to observed use.',
      skillSlug: 'platform-engineering__skills__iam-policy-minimiser',
    },
    {
      name: 'Incident Timeline Writer',
      description: 'Assembles an incident timeline from logs and alerts.',
      skillSlug: 'platform-engineering__skills__incident-timeline-writer',
    },
    {
      name: 'Ingress Route Mapper',
      description: 'Maps hostnames to services across namespaces.',
      skillSlug: 'platform-engineering__skills__ingress-route-mapper',
    },
    {
      name: 'Istio mTLS Checker',
      description: 'Reports workloads still accepting plaintext traffic.',
      skillSlug: 'platform-engineering__skills__istio-mtls-checker',
    },
    {
      name: 'Kafka Lag Diagnoser',
      description: 'Explains consumer lag and proposes remedies.',
      skillSlug: 'platform-engineering__skills__kafka-lag-diagnoser',
    },
    {
      name: 'Kubecost Reporter',
      description: 'Summarises namespace spend for a reporting period.',
      skillSlug: 'platform-engineering__skills__kubecost-reporter',
    },
    {
      name: 'Latency Budget Tracker',
      description: 'Tracks error budget burn against a service objective.',
      skillSlug: 'platform-engineering__skills__latency-budget-tracker',
    },
    {
      name: 'Load Test Planner',
      description: 'Designs a load profile from production traffic shape.',
      skillSlug: 'platform-engineering__skills__load-test-planner',
    },
    {
      name: 'Log Volume Trimmer',
      description: 'Identifies the noisiest log lines by ingest cost.',
      skillSlug: 'platform-engineering__skills__log-volume-trimmer',
    },
    {
      name: 'Migration Rehearsal',
      description: 'Dry-runs a schema migration and reports lock risk.',
      skillSlug: 'platform-engineering__skills__migration-rehearsal',
    },
    {
      name: 'Node Pool Rightsizer',
      description: 'Recommends instance types from utilisation history.',
      skillSlug: 'platform-engineering__skills__node-pool-rightsizer',
    },
    {
      name: 'On-Call Handover',
      description: "Drafts a handover note from the shift's incidents.",
      skillSlug: 'platform-engineering__skills__on-call-handover',
    },
    {
      name: 'OPA Policy Explainer',
      description: 'Explains why a request was denied by policy.',
      skillSlug: 'platform-engineering__skills__opa-policy-explainer',
    },
    {
      name: 'Postgres Index Advisor',
      description: 'Suggests indexes from slow query logs.',
      skillSlug: 'platform-engineering__skills__postgres-index-advisor',
    },
    {
      name: 'PR Risk Scorer',
      description: 'Scores a pull request by blast radius.',
      skillSlug: 'platform-engineering__skills__pr-risk-scorer',
    },
    {
      name: 'Prometheus Rule Linter',
      description: 'Reviews alerting rules for common mistakes.',
      skillSlug: 'platform-engineering__skills__prometheus-rule-linter',
    },
    {
      name: 'Redis Memory Profiler',
      description: 'Breaks down key space by memory use.',
      skillSlug: 'platform-engineering__skills__redis-memory-profiler',
    },
    {
      name: 'Release Notes Drafter',
      description: 'Assembles release notes from merged pull requests.',
      skillSlug: 'platform-engineering__skills__release-notes-drafter',
    },
    {
      name: 'Runbook Generator',
      description: 'Turns a resolved incident into a reusable runbook.',
      skillSlug: 'platform-engineering__skills__runbook-generator',
    },
    {
      name: 'SBOM Reviewer',
      description: 'Reviews a software bill of materials for known risk.',
      skillSlug: 'platform-engineering__skills__sbom-reviewer',
    },
    {
      name: 'Secret Rotation Planner',
      description: 'Sequences rotation across dependent services.',
      skillSlug: 'platform-engineering__skills__secret-rotation-planner',
    },
    {
      name: 'Secret Scanner',
      description: 'Finds credentials committed to a repository.',
      skillSlug: 'platform-engineering__skills__secret-scanner',
    },
    {
      name: 'Service Catalog Auditor',
      description: 'Flags services missing an owner or a tier.',
      skillSlug: 'platform-engineering__skills__service-catalog-auditor',
    },
    {
      name: 'SLO Drafter',
      description: 'Proposes service level objectives from latency history.',
      skillSlug: 'platform-engineering__skills__slo-drafter',
    },
    {
      name: 'Spot Interruption Planner',
      description: 'Assesses which workloads tolerate spot capacity.',
      skillSlug: 'platform-engineering__skills__spot-interruption-planner',
    },
    {
      name: 'SQL Query Reviewer',
      description: 'Reviews queries for full scans and N+1 patterns.',
      skillSlug: 'platform-engineering__skills__sql-query-reviewer',
    },
    {
      name: 'SSL Config Grader',
      description: 'Grades TLS configuration against current guidance.',
      skillSlug: 'platform-engineering__skills__ssl-config-grader',
    },
    {
      name: 'Terraform Cost Preview',
      description: 'Estimates the cost change from a Terraform plan.',
      skillSlug: 'platform-engineering__skills__terraform-cost-preview',
    },
    {
      name: 'Terraform Module Reviewer',
      description: 'Reviews module interfaces for reuse.',
      skillSlug: 'platform-engineering__skills__terraform-module-reviewer',
    },
    {
      name: 'Test Coverage Mapper',
      description: 'Maps untested code paths to recent incidents.',
      skillSlug: 'platform-engineering__skills__test-coverage-mapper',
    },
    {
      name: 'Toil Estimator',
      description: 'Quantifies repetitive operational work per week.',
      skillSlug: 'platform-engineering__skills__toil-estimator',
    },
    {
      name: 'Trace Sampler Tuner',
      description: 'Recommends sampling rates from trace volume.',
      skillSlug: 'platform-engineering__skills__trace-sampler-tuner',
    },
    {
      name: 'Vault Policy Reviewer',
      description: 'Reviews Vault policies for least privilege.',
      skillSlug: 'platform-engineering__skills__vault-policy-reviewer',
    },
    {
      name: 'VPC Peering Mapper',
      description: 'Diagrams cross-account network reachability.',
      skillSlug: 'platform-engineering__skills__vpc-peering-mapper',
    },
    {
      name: 'Webhook Replay Tool',
      description: 'Replays a failed webhook delivery against staging.',
      skillSlug: 'platform-engineering__skills__webhook-replay-tool',
    },
    {
      name: 'Terraform Audit A',
      description: 'Audits Terraform state for drift. Variant A.',
      skillSlug: 'platform-engineering__skills__terraform-audit-a',
    },
    {
      name: 'Terraform Audit B',
      description: 'Audits Terraform state for drift. Variant B.',
      skillSlug: 'platform-engineering__skills__terraform-audit-b',
    },
  ],
};

/** The full Skills, built from the lists. Each Skill has only its SKILL.md. */
export const SKILL_DETAIL_FIXTURES: Skill[] = Object.values(SKILL_FIXTURES_BY_MARKETPLACE).flatMap(
  (skills) =>
    skills.map((summary) => {
      const folder = summary.skillSlug.split('__').pop() ?? summary.skillSlug;
      return {
        ...summary,
        skill: {
          path: `skills/${folder}/SKILL.md`,
          name: 'SKILL.md',
          content: `# ${summary.name}\n\n${summary.description}`,
        },
        documents: [],
      };
    }),
);
