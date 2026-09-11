/**
 * ---
 * generated_by: claude
 * human_requestor: stoyan.grozdev@clearroute.io
 * date: 2026-09-10
 * review_status: unreviewed
 * reviewed_by:
 * ---
 */
import 'server-only';

import type { Marketplace, PackageDetail, PackageOverview } from '@orbit/shared/api';

export const MARKETPLACE_FIXTURES: Marketplace[] = [
  { name: 'Platform Engineering', org: 'platform-engineering', repo: 'skills', branch: 'main' },
  { name: 'ClearRoute Core', org: 'clearroute', repo: 'core-skills', branch: 'main' },
  { name: 'ClearRoute Platform', org: 'clearroute', repo: 'platform-skills', branch: 'main' },
  { name: 'Partner Reference', org: 'partner', repo: 'reference-skills', branch: 'main' },
];
// ⟦REVIEW⟧

export const PACKAGE_FIXTURES_BY_MARKETPLACE: Record<string, PackageOverview[]> = {
  'platform-engineering/skills': [
    {
      slug: 'platform-engineering/skills/react-expert',
      title_of_package: 'React Expert',
      description: 'Senior React developer.',
    },
    {
      slug: 'platform-engineering/skills/react-reviewer',
      title_of_package: 'React Reviewer',
      description: 'Reviews React components for accessibility and state handling.',
    },
    {
      slug: 'platform-engineering/skills/airflow-access-review',
      title_of_package: 'Airflow Access Review',
      description: 'List who can reach Airflow and flag grants with no named owner.',
    },
    {
      slug: 'platform-engineering/skills/alert-noise-reducer',
      title_of_package: 'Alert Noise Reducer',
      description: 'Groups duplicate alerts and proposes threshold changes.',
    },
    {
      slug: 'platform-engineering/skills/ansible-drift-check',
      title_of_package: 'Ansible Drift Check',
      description: 'Compares live host state against playbook intent.',
    },
    {
      slug: 'platform-engineering/skills/api-contract-diff',
      title_of_package: 'API Contract Diff',
      description: 'Diffs OpenAPI specifications and flags breaking changes.',
    },
    {
      slug: 'platform-engineering/skills/argo-sync-auditor',
      title_of_package: 'Argo Sync Auditor',
      description: 'Reports applications that are out of sync with Git.',
    },
    {
      slug: 'platform-engineering/skills/backup-restore-drill',
      title_of_package: 'Backup Restore Drill',
      description: 'Walks a restore rehearsal and records the outcome.',
    },
    {
      slug: 'platform-engineering/skills/bazel-cache-analyst',
      title_of_package: 'Bazel Cache Analyst',
      description: 'Explains cache misses in a build graph.',
    },
    {
      slug: 'platform-engineering/skills/bicep-linter',
      title_of_package: 'Bicep Linter',
      description: 'Reviews Azure Bicep templates for policy violations.',
    },
    {
      slug: 'platform-engineering/skills/budget-forecaster',
      title_of_package: 'Budget Forecaster',
      description: 'Projects month-end cloud spend from the current burn rate.',
    },
    {
      slug: 'platform-engineering/skills/cert-expiry-sweep',
      title_of_package: 'Cert Expiry Sweep',
      description: 'Lists TLS certificates expiring within thirty days.',
    },
    {
      slug: 'platform-engineering/skills/ci-flake-hunter',
      title_of_package: 'CI Flake Hunter',
      description: 'Finds tests that fail intermittently across runs.',
    },
    {
      slug: 'platform-engineering/skills/cluster-upgrade-planner',
      title_of_package: 'Cluster Upgrade Planner',
      description: 'Sequences a Kubernetes version upgrade.',
    },
    {
      slug: 'platform-engineering/skills/cost-anomaly-explainer',
      title_of_package: 'Cost Anomaly Explainer',
      description: 'Attributes a spend spike to a specific service change.',
    },
    {
      slug: 'platform-engineering/skills/dashboard-gap-finder',
      title_of_package: 'Dashboard Gap Finder',
      description: 'Flags services with no latency dashboard.',
    },
    {
      slug: 'platform-engineering/skills/data-retention-auditor',
      title_of_package: 'Data Retention Auditor',
      description: 'Checks table retention settings against policy.',
    },
    {
      slug: 'platform-engineering/skills/dns-record-reviewer',
      title_of_package: 'DNS Record Reviewer',
      description: 'Finds dangling and duplicate DNS records.',
    },
    {
      slug: 'platform-engineering/skills/docker-image-slimmer',
      title_of_package: 'Docker Image Slimmer',
      description: 'Proposes smaller base images and merged layers.',
    },
    {
      slug: 'platform-engineering/skills/envoy-config-explainer',
      title_of_package: 'Envoy Config Explainer',
      description: 'Explains an Envoy listener chain in plain terms.',
    },
    {
      slug: 'platform-engineering/skills/feature-flag-reaper',
      title_of_package: 'Feature Flag Reaper',
      description: 'Lists flags fully rolled out and safe to remove.',
    },
    {
      slug: 'platform-engineering/skills/github-action-auditor',
      title_of_package: 'GitHub Action Auditor',
      description: 'Reviews workflow permissions and unpinned actions.',
    },
    {
      slug: 'platform-engineering/skills/grafana-panel-builder',
      title_of_package: 'Grafana Panel Builder',
      description: "Drafts dashboard panels from a service's metric names.",
    },
    {
      slug: 'platform-engineering/skills/helm-values-differ',
      title_of_package: 'Helm Values Differ',
      description: 'Diffs rendered Helm output between two releases.',
    },
    {
      slug: 'platform-engineering/skills/iam-policy-minimiser',
      title_of_package: 'IAM Policy Minimiser',
      description: 'Narrows over-broad IAM policies to observed use.',
    },
    {
      slug: 'platform-engineering/skills/incident-timeline-writer',
      title_of_package: 'Incident Timeline Writer',
      description: 'Assembles an incident timeline from logs and alerts.',
    },
    {
      slug: 'platform-engineering/skills/ingress-route-mapper',
      title_of_package: 'Ingress Route Mapper',
      description: 'Maps hostnames to services across namespaces.',
    },
    {
      slug: 'platform-engineering/skills/istio-mtls-checker',
      title_of_package: 'Istio mTLS Checker',
      description: 'Reports workloads still accepting plaintext traffic.',
    },
    {
      slug: 'platform-engineering/skills/kafka-lag-diagnoser',
      title_of_package: 'Kafka Lag Diagnoser',
      description: 'Explains consumer lag and proposes remedies.',
    },
    {
      slug: 'platform-engineering/skills/kubecost-reporter',
      title_of_package: 'Kubecost Reporter',
      description: 'Summarises namespace spend for a reporting period.',
    },
    {
      slug: 'platform-engineering/skills/latency-budget-tracker',
      title_of_package: 'Latency Budget Tracker',
      description: 'Tracks error budget burn against a service objective.',
    },
    {
      slug: 'platform-engineering/skills/load-test-planner',
      title_of_package: 'Load Test Planner',
      description: 'Designs a load profile from production traffic shape.',
    },
    {
      slug: 'platform-engineering/skills/log-volume-trimmer',
      title_of_package: 'Log Volume Trimmer',
      description: 'Identifies the noisiest log lines by ingest cost.',
    },
    {
      slug: 'platform-engineering/skills/migration-rehearsal',
      title_of_package: 'Migration Rehearsal',
      description: 'Dry-runs a schema migration and reports lock risk.',
    },
    {
      slug: 'platform-engineering/skills/node-pool-rightsizer',
      title_of_package: 'Node Pool Rightsizer',
      description: 'Recommends instance types from utilisation history.',
    },
    {
      slug: 'platform-engineering/skills/on-call-handover',
      title_of_package: 'On-Call Handover',
      description: "Drafts a handover note from the shift's incidents.",
    },
    {
      slug: 'platform-engineering/skills/opa-policy-explainer',
      title_of_package: 'OPA Policy Explainer',
      description: 'Explains why a request was denied by policy.',
    },
    {
      slug: 'platform-engineering/skills/postgres-index-advisor',
      title_of_package: 'Postgres Index Advisor',
      description: 'Suggests indexes from slow query logs.',
    },
    {
      slug: 'platform-engineering/skills/pr-risk-scorer',
      title_of_package: 'PR Risk Scorer',
      description: 'Scores a pull request by blast radius.',
    },
    {
      slug: 'platform-engineering/skills/prometheus-rule-linter',
      title_of_package: 'Prometheus Rule Linter',
      description: 'Reviews alerting rules for common mistakes.',
    },
    {
      slug: 'platform-engineering/skills/redis-memory-profiler',
      title_of_package: 'Redis Memory Profiler',
      description: 'Breaks down key space by memory use.',
    },
    {
      slug: 'platform-engineering/skills/release-notes-drafter',
      title_of_package: 'Release Notes Drafter',
      description: 'Assembles release notes from merged pull requests.',
    },
    {
      slug: 'platform-engineering/skills/runbook-generator',
      title_of_package: 'Runbook Generator',
      description: 'Turns a resolved incident into a reusable runbook.',
    },
    {
      slug: 'platform-engineering/skills/sbom-reviewer',
      title_of_package: 'SBOM Reviewer',
      description: 'Reviews a software bill of materials for known risk.',
    },
    {
      slug: 'platform-engineering/skills/secret-rotation-planner',
      title_of_package: 'Secret Rotation Planner',
      description: 'Sequences rotation across dependent services.',
    },
    {
      slug: 'platform-engineering/skills/secret-scanner',
      title_of_package: 'Secret Scanner',
      description: 'Finds credentials committed to a repository.',
    },
    {
      slug: 'platform-engineering/skills/service-catalog-auditor',
      title_of_package: 'Service Catalog Auditor',
      description: 'Flags services missing an owner or a tier.',
    },
    {
      slug: 'platform-engineering/skills/slo-drafter',
      title_of_package: 'SLO Drafter',
      description: 'Proposes service level objectives from latency history.',
    },
    {
      slug: 'platform-engineering/skills/spot-interruption-planner',
      title_of_package: 'Spot Interruption Planner',
      description: 'Assesses which workloads tolerate spot capacity.',
    },
    {
      slug: 'platform-engineering/skills/sql-query-reviewer',
      title_of_package: 'SQL Query Reviewer',
      description: 'Reviews queries for full scans and N+1 patterns.',
    },
    {
      slug: 'platform-engineering/skills/ssl-config-grader',
      title_of_package: 'SSL Config Grader',
      description: 'Grades TLS configuration against current guidance.',
    },
    {
      slug: 'platform-engineering/skills/terraform-cost-preview',
      title_of_package: 'Terraform Cost Preview',
      description: 'Estimates the cost change from a Terraform plan.',
    },
    {
      slug: 'platform-engineering/skills/terraform-module-reviewer',
      title_of_package: 'Terraform Module Reviewer',
      description: 'Reviews module interfaces for reuse.',
    },
    {
      slug: 'platform-engineering/skills/test-coverage-mapper',
      title_of_package: 'Test Coverage Mapper',
      description: 'Maps untested code paths to recent incidents.',
    },
    {
      slug: 'platform-engineering/skills/toil-estimator',
      title_of_package: 'Toil Estimator',
      description: 'Quantifies repetitive operational work per week.',
    },
    {
      slug: 'platform-engineering/skills/trace-sampler-tuner',
      title_of_package: 'Trace Sampler Tuner',
      description: 'Recommends sampling rates from trace volume.',
    },
    {
      slug: 'platform-engineering/skills/vault-policy-reviewer',
      title_of_package: 'Vault Policy Reviewer',
      description: 'Reviews Vault policies for least privilege.',
    },
    {
      slug: 'platform-engineering/skills/vpc-peering-mapper',
      title_of_package: 'VPC Peering Mapper',
      description: 'Diagrams cross-account network reachability.',
    },
    {
      slug: 'platform-engineering/skills/webhook-replay-tool',
      title_of_package: 'Webhook Replay Tool',
      description: 'Replays a failed webhook delivery against staging.',
    },
    {
      slug: 'platform-engineering/skills/terraform-audit-a',
      title_of_package: 'Terraform Audit',
      description: 'Audits Terraform state for drift. Variant A.',
    },
    {
      slug: 'platform-engineering/skills/terraform-audit-b',
      title_of_package: 'Terraform Audit',
      description: 'Audits Terraform state for drift. Variant B.',
    },
  ],
  'clearroute/core-skills': [
    {
      slug: 'clearroute/core-skills/incident-triage',
      title_of_package: 'Incident Triage',
      description: 'Triages incoming incidents by severity.',
    },
    {
      slug: 'clearroute/core-skills/change-freeze-checker',
      title_of_package: 'Change Freeze Checker',
      description: 'Reports whether a change window is currently open.',
    },
    {
      slug: 'clearroute/core-skills/customer-impact-summariser',
      title_of_package: 'Customer Impact Summariser',
      description: 'Estimates affected customers from an incident.',
    },
    {
      slug: 'clearroute/core-skills/escalation-router',
      title_of_package: 'Escalation Router',
      description: 'Routes an incident to the correct on-call rota.',
    },
    {
      slug: 'clearroute/core-skills/postmortem-reviewer',
      title_of_package: 'Postmortem Reviewer',
      description: 'Checks a postmortem for action-item quality.',
    },
    {
      slug: 'clearroute/core-skills/status-page-drafter',
      title_of_package: 'Status Page Drafter',
      description: 'Drafts a customer-facing status update.',
    },
  ],
  'clearroute/platform-skills': [
    {
      slug: 'clearroute/platform-skills/incident-triage',
      title_of_package: 'Incident Triage',
      description: 'Triages incoming incidents by owning team.',
    },
    {
      slug: 'clearroute/platform-skills/cost-report',
      title_of_package: 'Cost Report',
      description: 'Summarises spend by environment.',
    },
  ],
  'partner/reference-skills': [],
};
// ⟦REVIEW⟧

export const PACKAGE_DETAIL_FIXTURES: PackageDetail[] = [
  {
    slug: 'platform-engineering/skills/react-expert',
    title_of_package: 'React Expert',
    description: 'Senior React developer.',
    package: {
      path_of_package_in_repo: 'skills/react-expert/SKILL.md',
      name_of_package: 'SKILL.md',
      body_of_package: '# React Expert\n\n## When to use this skill\nSenior React developer.',
    },
    resources_of_package: [
      {
        path_of_resource_in_repo: 'skills/react-expert/resources/examples.md',
        name_of_resource: 'examples.md',
        body_of_resource: '# examples.md\n\nExample content for React Expert.',
      },
      {
        path_of_resource_in_repo: 'skills/react-expert/resources/patterns.md',
        name_of_resource: 'patterns.md',
        body_of_resource: '# patterns.md\n\nExample content for React Expert.',
      },
    ],
    name_of_marketplace: 'Platform Engineering',
  },
  {
    slug: 'platform-engineering/skills/react-reviewer',
    title_of_package: 'React Reviewer',
    description: 'Reviews React components for accessibility and state handling.',
    package: {
      path_of_package_in_repo: 'skills/react-reviewer/SKILL.md',
      name_of_package: 'SKILL.md',
      body_of_package:
        '# React Reviewer\n\n## When to use this skill\nReviews React components for accessibility and state handling.',
    },
    resources_of_package: [],
    name_of_marketplace: 'Platform Engineering',
  },
  {
    slug: 'platform-engineering/skills/terraform-audit-a',
    title_of_package: 'Terraform Audit',
    description: 'Audits Terraform state for drift. Variant A.',
    package: {
      path_of_package_in_repo: 'skills/terraform-audit-a/SKILL.md',
      name_of_package: 'SKILL.md',
      body_of_package:
        '# Terraform Audit\n\n## When to use this skill\nAudits Terraform state for drift. Variant A.',
    },
    resources_of_package: [
      {
        path_of_resource_in_repo: 'skills/terraform-audit-a/resources/examples.md',
        name_of_resource: 'examples.md',
        body_of_resource: '# examples.md\n\nExample content for Terraform Audit.',
      },
    ],
    name_of_marketplace: 'Platform Engineering',
  },
  {
    slug: 'platform-engineering/skills/terraform-audit-b',
    title_of_package: 'Terraform Audit',
    description: 'Audits Terraform state for drift. Variant B.',
    package: {
      path_of_package_in_repo: 'skills/terraform-audit-b/SKILL.md',
      name_of_package: 'SKILL.md',
      body_of_package:
        '# Terraform Audit\n\n## When to use this skill\nAudits Terraform state for drift. Variant B.',
    },
    resources_of_package: [
      {
        path_of_resource_in_repo: 'skills/terraform-audit-b/resources/examples.md',
        name_of_resource: 'examples.md',
        body_of_resource: '# examples.md\n\nExample content for Terraform Audit.',
      },
    ],
    name_of_marketplace: 'Platform Engineering',
  },
  {
    slug: 'clearroute/core-skills/incident-triage',
    title_of_package: 'Incident Triage',
    description: 'Triages incoming incidents by severity.',
    package: {
      path_of_package_in_repo: 'skills/incident-triage/SKILL.md',
      name_of_package: 'SKILL.md',
      body_of_package:
        '# Incident Triage\n\n## When to use this skill\nTriages incoming incidents by severity.',
    },
    resources_of_package: [
      {
        path_of_resource_in_repo: 'skills/incident-triage/resources/runbook.md',
        name_of_resource: 'runbook.md',
        body_of_resource: '# runbook.md\n\nExample content for Incident Triage.',
      },
    ],
    name_of_marketplace: 'ClearRoute Core',
  },
  {
    slug: 'clearroute/platform-skills/incident-triage',
    title_of_package: 'Incident Triage',
    description: 'Triages incoming incidents by owning team.',
    package: {
      path_of_package_in_repo: 'skills/incident-triage/SKILL.md',
      name_of_package: 'SKILL.md',
      body_of_package:
        '# Incident Triage\n\n## When to use this skill\nTriages incoming incidents by owning team.',
    },
    resources_of_package: [
      {
        path_of_resource_in_repo: 'skills/incident-triage/resources/runbook.md',
        name_of_resource: 'runbook.md',
        body_of_resource: '# runbook.md\n\nExample content for Incident Triage.',
      },
    ],
    name_of_marketplace: 'ClearRoute Platform',
  },
];
// ⟦REVIEW⟧
