'use client';

/** @tier 3 — agents/[slug]-scoped. The Configure modal: the builder's rail +
 * section anatomy applied to editing, on the shared motif components. One
 * dirty state, one save, one re-provision
 * (docs/adr/2026-07-17-agent-detail-configuration.md). */

import { useState } from 'react';
import {
  Alert,
  Anchor,
  Badge,
  Callout,
  Button,
  DangerZone,
  Eyebrow,
  FormField,
  Grid,
  Heading,
  Icon,
  Input,
  Modal,
  ModalFooter,
  ModalHeader,
  SectionLabel as FieldLabel,
  Separator,
  Stack,
  StatusDot,
  Text,
  Textarea,
  ConfirmDialog,
  Wizard,
} from '@runkora/hades';
import { useBrand } from '@runkora/theme/brand-provider';
import type { AgentFramework, ConnectionCatalogEntry, ModelObject } from '@runkora/shared/api';
import { FrameworkCards } from '../../_components/motif/framework-cards';
import { IDENTITY_MODE_SUMMARIES } from '../../_components/motif/identity-mode-cards';
import { ModelPicker } from '@runkora/components';
import {
  McpToolTree,
  countGrantedServers,
  withGrantedScopes,
} from '../../_components/motif/mcp-tool-tree';
import { ConnectionCards } from '@runkora/components';
import { EgressModeCards } from '../../_components/motif/egress-mode-cards';
import { EnvironmentsCard } from '../../_components/motif/environments-card';
import { GuardrailsFields } from '../../_components/motif/guardrail-rows';
import type { GuardrailValues } from '../../_components/motif/guardrail-rows';
import { PlannedLimits } from '../../_components/motif/planned-limits';
import { EffectiveAccessPanel, PermissionHint, permissionHintText } from '@runkora/components';
import { managedInGit as isManagedInGit, repositoryUrl } from '@runkora/lib/declarative';
import { useTenantLocked } from '../../../_components/permissions-provider';
import { CONFIG_SECTIONS, ConfigurationRail } from './configuration-rail';
import { useAgentConfiguration } from '../_hooks/use-agent-configuration';

function SectionLabel({ title, desc }: { title: string; desc: string }) {
  return (
    <Stack gap="tight">
      <Text as="span" size="caption" weight="heavy" color="muted">
        {title}
      </Text>
      <Text size="micro" color="subtle">
        {desc}
      </Text>
    </Stack>
  );
}

/** A field fixed at creation: the label carries a Read-only pill, as the trigger modal does. */
function ImmutableField({
  label,
  hint,
  pill,
  children,
}: {
  label: string;
  hint: string;
  /** False when the whole form is read only, where the header says so once. */
  pill: boolean;
  children: React.ReactNode;
}) {
  return (
    <Stack gap="snug">
      <Stack direction="horizontal" align="center" gap="snug">
        <FieldLabel>{label}</FieldLabel>
        {pill && (
          <Badge variant="dim" size="sm" shape="pill">
            Read-only
          </Badge>
        )}
      </Stack>
      {children}
      <Text size="caption" color="subtle">
        {hint}
      </Text>
    </Stack>
  );
}

/** Heads the modal of an agent managed in git: the one place that says every field is read only. */
function ManagedBanner({ repository }: { repository: string | undefined }) {
  const url = repository ? repositoryUrl(repository) : null;
  return (
    <Stack paddingInline="comfortable" paddingBlock="cozy">
      <Callout aria-label="Managed in git">
        <Stack direction="horizontal" align="center" gap="cozy">
          <Icon name="git-branch" size="md" container containerVariant="surface-subtle" label="" />
          <Stack gap="tight">
            <Text as="span" size="supporting" weight="heavy">
              Managed in git
            </Text>
            <Text size="supporting" color="muted">
              This agent&apos;s configuration is managed in git. This is a read-only config view.
              Make changes in{' '}
              {repository ? (
                url ? (
                  <Anchor href={url} target="_blank" rel="noopener noreferrer">
                    {repository}
                  </Anchor>
                ) : (
                  repository
                )
              ) : (
                'the configuration repository'
              )}
              .
            </Text>
          </Stack>
        </Stack>
      </Callout>
    </Stack>
  );
}

interface AgentConfigureModalProps {
  open: boolean;
  /** The rail step to open on, e.g. Behaviour from the Instructions card. */
  initialSection?: string | undefined;
  onClose: () => void;
}

/** Mounts the form only while open, so each open starts from the current agent with no stale edits. */
export function AgentConfigureModal({ open, initialSection, onClose }: AgentConfigureModalProps) {
  if (!open) return null;
  return <ConfigureBody initialSection={initialSection ?? 'identity'} onClose={onClose} />;
}

function ConfigureBody({
  initialSection,
  onClose,
}: {
  initialSection: string;
  onClose: () => void;
}) {
  const cfg = useAgentConfiguration();
  const { productName } = useBrand();
  const [active, setActive] = useState(initialSection);
  const [discardOpen, setDiscardOpen] = useState(false);
  const locked = useTenantLocked();

  if (!cfg.agent) return null;
  const agent = cfg.agent;
  // A declared agent, or any agent in a locked tenant, changes only in the configuration repository.
  const managedInGit = isManagedInGit(agent.declared, locked);
  // Without edit access, or managed in git, every field is read only; the notice above the form says why.
  const readOnly = !agent.canEdit || managedInGit;
  const dirty = !readOnly && cfg.isDirty;
  // An agent managed in git is deleted in its repository, so it has no Danger zone.
  const sections = managedInGit
    ? CONFIG_SECTIONS.filter((x) => x.tone !== 'danger')
    : CONFIG_SECTIONS;
  // What the agent holds can be missing from the catalogues the viewer may read: a model they cannot
  // use, tools or connections they cannot list. A read-only view shows the stored grants regardless.
  const modelOptions: ModelObject[] =
    cfg.modelId && !cfg.models.some((m) => m.id === cfg.modelId)
      ? [
          ...cfg.models,
          {
            id: cfg.modelId,
            object: 'model',
            created: 0,
            owned_by: '',
            family: '',
            display_name: cfg.modelId,
            status: 'available',
            aliases: [],
            input_modalities: [],
            output_modalities: [],
            dialects: [],
          },
        ]
      : cfg.models;
  const mcpCatalog = readOnly ? withGrantedScopes(cfg.mcpCatalog, cfg.mcpTools) : cfg.mcpCatalog;
  const connections = readOnly
    ? [
        ...cfg.availableConnections,
        ...cfg.connectors
          .filter((c) => !cfg.availableConnections.some((a) => a.name === c.connection))
          .map((c): ConnectionCatalogEntry => ({
            name: c.connection,
            adapter: '',
            actions: c.actions,
            baseline: [],
            qualifier: c.resources?.length ? { name: 'resource', description: '' } : null,
          })),
      ]
    : cfg.availableConnections;
  const activeSection = sections.find((x) => x.id === active) ?? sections[0]!;
  const descriptions: Record<string, string> = {
    identity:
      'Who this agent is on the control plane. The slug is its permanent identity and cannot change.',
    behaviour: 'How the agent behaves, and the model and framework it runs on.',
    tools:
      'The MCP servers and connections this agent may call. Every call is checked before it runs.',
    network: 'The hosts the agent may reach, and the environments it can run in.',
    guardrails: `Set the ceilings ${productName} enforces at runtime: spend, capacity and loop limits.`,
    danger: 'Destructive actions. These cannot be undone.',
  };
  // Escape, the overlay and × all land here: unsaved edits ask before they are lost.
  // A save in flight cannot be taken back, so nothing closes the modal until it lands.
  const handleClose = () => {
    if (cfg.saving) return;
    if (dirty) setDiscardOpen(true);
    else onClose();
  };

  const guardrailValues: GuardrailValues = {
    agentBudgetOn: cfg.budgetLimitUSD !== '',
    agentBudgetUsd: cfg.budgetLimitUSD || '500.00',
    agentBudgetCadence: cfg.budgetCadence === 'weekly' ? 'weekly' : 'monthly',
    runBudgetOn: cfg.budgetPerRunUSD !== '',
    runBudgetUsd: cfg.budgetPerRunUSD || '5.00',
    capacityOn: true,
    capacity: cfg.maxConcurrentRuns,
    ...cfg.loopGuardrails,
  };

  const onGuardrailChange = <K extends keyof GuardrailValues>(
    key: K,
    value: GuardrailValues[K],
  ) => {
    switch (key) {
      case 'agentBudgetOn':
        cfg.setBudgetLimitUSD(value ? '500.00' : '');
        break;
      case 'agentBudgetUsd':
        cfg.setBudgetLimitUSD(String(value));
        break;
      case 'agentBudgetCadence':
        cfg.setBudgetCadence(String(value));
        break;
      case 'runBudgetOn':
        cfg.setBudgetPerRunUSD(value ? '5.00' : '');
        break;
      case 'runBudgetUsd':
        cfg.setBudgetPerRunUSD(String(value));
        break;
      case 'capacityOn':
        // Locked on: capacity always has a stored value, so there is nothing to switch off.
        break;
      case 'capacity':
        cfg.setMaxConcurrentRuns(String(value));
        break;
      case 'repeatedCallOn':
      case 'waitRearmOn':
        cfg.setLoopGuardrail(key, Boolean(value));
        break;
      case 'repeatedCallWarn':
      case 'repeatedCallPause':
      case 'waitRearmWarn':
      case 'waitRearmPause':
        cfg.setLoopGuardrail(key, String(value));
        break;
    }
  };

  return (
    <>
      <Modal open onClose={handleClose} size="builder">
        <ModalHeader
          title="Configuration"
          description={`${agent.name} · ${agent.slug}`}
          titleAccessory={
            readOnly ? (
              <Badge variant="reserved" size="sm" shape="pill">
                <Icon name="lock" size="2xs" label="" />
                Read-only
              </Badge>
            ) : dirty ? (
              <Stack direction="horizontal" align="center" gap="snug">
                <StatusDot size="sm" status="degraded" pulse />
                <Text as="span" size="caption" color="muted">
                  Unsaved changes
                </Text>
              </Stack>
            ) : undefined
          }
          borderless={false}
        />
        {managedInGit && <ManagedBanner repository={agent.repository} />}
        <Stack direction={{ base: 'vertical', lg: 'horizontal' }} grow overflow="hidden" gap="none">
          <ConfigurationRail
            label="Configuration"
            sections={sections}
            current={active}
            onSelect={setActive}
          />
          <Wizard.Step>
            <Stack gap="snug">
              <Heading variant="subsection">{activeSection.title}</Heading>
              <Text size="supporting" color="muted">
                {descriptions[activeSection.id]}
              </Text>
            </Stack>
            {!managedInGit && readOnly && (
              <Alert variant="info">
                <Text size="caption">
                  {permissionHintText('change this agent')} Changing it needs the agents:types:
                  {agent.identityMode}:write scope
                  {agent.identityMode === 'autonomous' ? ' and every scope the agent holds.' : '.'}
                </Text>
              </Alert>
            )}
            {/* ── Identity ─────────────────────────────────────────────────────── */}
            {activeSection.id === 'identity' && (
              <Stack gap="relaxed">
                <Grid cols={2} gap="comfortable">
                  <FormField label="Display name" required>
                    <Input
                      disabled={readOnly}
                      value={cfg.name}
                      onChange={(e) => cfg.setName(e.target.value)}
                      maxLength={128}
                    />
                  </FormField>
                  <ImmutableField
                    label="Slug"
                    hint="Cannot be changed after creation."
                    pill={!readOnly}
                  >
                    <Input mono disabled value={agent.slug} aria-label="Slug (immutable)" />
                  </ImmutableField>
                </Grid>
                <FormField
                  label="Description"
                  hint="Optional. Shown in the catalogue and on its identity card."
                >
                  <Textarea
                    disabled={readOnly}
                    value={cfg.description}
                    onChange={(e) => cfg.setDescription(e.target.value)}
                    rows={2}
                  />
                </FormField>
                {/* The identity model is immutable — display only, like the slug. */}
                <ImmutableField
                  label="Identity model"
                  pill={!readOnly}
                  hint="Chosen at creation and cannot be changed. Recreate the agent to use the other model."
                >
                  <Input
                    disabled
                    value={IDENTITY_MODE_SUMMARIES[agent.identityMode]}
                    aria-label="Identity model (immutable)"
                  />
                </ImmutableField>
              </Stack>
            )}

            {/* ── Behaviour ────────────────────────────────────────────────────── */}
            {activeSection.id === 'behaviour' && (
              <Stack gap="relaxed">
                <FormField label="System instructions" required>
                  <Textarea
                    disabled={readOnly}
                    value={cfg.systemPrompt}
                    onChange={(e) => cfg.setSystemPrompt(e.target.value)}
                    rows={5}
                    resize="vertical"
                  />
                </FormField>
                <Separator />
                <Stack gap="cozy">
                  <Text as="span" size="caption" weight="heavy" color="muted">
                    Framework
                  </Text>
                  <FrameworkCards
                    disabled={readOnly}
                    value={cfg.framework}
                    onChange={(v: AgentFramework) => cfg.setFramework(v)}
                  />
                </Stack>
                <Stack gap="cozy">
                  <Stack direction="horizontal" align="center">
                    <Text as="span" size="caption" weight="heavy" color="muted">
                      Model
                    </Text>
                    <Stack grow />
                    {!readOnly && (
                      <Text as="span" size="micro" color="subtle">
                        {cfg.models.length} enabled by your platform
                      </Text>
                    )}
                  </Stack>
                  {!readOnly && cfg.modelsError && (
                    <Alert variant="error">Models failed to load: {cfg.modelsError}</Alert>
                  )}
                  <ModelPicker
                    disabled={readOnly}
                    models={modelOptions}
                    value={cfg.modelId}
                    onChange={cfg.setModelId}
                  />
                </Stack>
              </Stack>
            )}

            {/* ── Tools & access ───────────────────────────────────────────────── */}
            {activeSection.id === 'tools' && (
              <Stack gap="relaxed">
                <Text as="span" size="supporting" color="muted">
                  <Text as="span" size="supporting" weight="heavy" color="accent">
                    {countGrantedServers(cfg.mcpServers, mcpCatalog, cfg.mcpTools)}
                  </Text>{' '}
                  servers ·{' '}
                  <Text as="span" size="supporting" weight="heavy">
                    {cfg.mcpTools.length}
                  </Text>{' '}
                  tools granted
                </Text>
                {!readOnly && cfg.mcpError && (
                  <Alert variant="error">MCP servers failed to load: {cfg.mcpError}</Alert>
                )}
                <McpToolTree
                  disabled={readOnly}
                  servers={cfg.mcpServers}
                  catalog={mcpCatalog}
                  granted={cfg.mcpTools}
                  onChange={cfg.setMcpTools}
                />
                <Separator />
                <Stack gap="cozy">
                  <SectionLabel
                    title="Connections"
                    desc="Credentialed connections the agent may call. Credentials are injected just-in-time. The agent never sees them."
                  />
                  <ConnectionCards
                    disabled={readOnly}
                    available={connections}
                    value={cfg.connectors}
                    onChange={cfg.setConnectors}
                  />
                </Stack>
              </Stack>
            )}

            {/* ── Network access ───────────────────────────────────────────────── */}
            {activeSection.id === 'network' && (
              <Stack gap="relaxed">
                <Stack gap="cozy">
                  <SectionLabel
                    title="Network egress"
                    desc="The hosts the agent may reach directly."
                  />
                  <EgressModeCards
                    disabled={readOnly}
                    mode={cfg.connectivityMode}
                    hosts={cfg.connectivityHosts}
                    onModeChange={cfg.setConnectivityMode}
                    onHostsChange={cfg.setConnectivityHosts}
                  />
                </Stack>
                <Stack gap="cozy">
                  <SectionLabel
                    title="Environments"
                    desc="The sandboxes this agent can run in. Their egress hosts and connections join the agent's effective access."
                  />
                  <EnvironmentsCard
                    disabled={readOnly}
                    environments={cfg.environmentCatalogue.data}
                    value={cfg.environments}
                    onChange={cfg.setEnvironments}
                    loading={cfg.environmentCatalogue.status === 'loading'}
                    error={cfg.environmentCatalogue.error}
                  />
                </Stack>
                <EffectiveAccessPanel
                  access={cfg.effectiveAccess}
                  loading={!cfg.effectiveAccess && !cfg.accessError}
                  error={cfg.accessError}
                />
              </Stack>
            )}

            {/* ── Guardrails ───────────────────────────────────────────────────── */}
            {activeSection.id === 'guardrails' && (
              <Stack gap="relaxed">
                <Stack direction="horizontal" align="baseline" gap="snug">
                  <Eyebrow>Limits</Eyebrow>
                  <Text as="span" size="micro" color="subtle">
                    Every limit is optional. Switch on the ones you want {productName} to enforce.
                  </Text>
                </Stack>
                <GuardrailsFields
                  capacityAlwaysOn
                  disabled={readOnly}
                  values={guardrailValues}
                  onChange={onGuardrailChange}
                />
                <PlannedLimits />
              </Stack>
            )}

            {/* ── Danger zone ──────────────────────────────────────────────────── */}
            {activeSection.id === 'danger' && (
              <Stack gap="relaxed">
                <DangerZone
                  title="Delete this agent"
                  description="Deregisters the agent, tears down its workload, and terminates all associated runs."
                  action={
                    <PermissionHint allowed={agent.canDelete} action="delete this agent">
                      <Button
                        variant="destructive"
                        size="xs"
                        disabled={!agent.canDelete}
                        onClick={() => cfg.setDeleteOpen(true)}
                      >
                        Delete agent
                      </Button>
                    </PermissionHint>
                  }
                />
              </Stack>
            )}
          </Wizard.Step>
        </Stack>
        <Separator />
        <ModalFooter justify="between">
          <Stack direction="horizontal" align="center" gap="snug">
            {cfg.saveSuccess ? (
              <>
                <StatusDot size="sm" status="healthy" />
                <Text as="span" size="caption">
                  Changes saved
                </Text>
              </>
            ) : cfg.saveError ? (
              <Text as="span" size="caption" color="status-error">
                {cfg.saveError}
              </Text>
            ) : (
              dirty && (
                <Text as="span" size="caption" color="subtle">
                  Saving re-provisions the agent and recompiles its grants before they apply.
                </Text>
              )
            )}
          </Stack>
          <Stack direction="horizontal" align="center" gap="snug">
            {!readOnly && (
              <>
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={cfg.handleDiscard}
                  disabled={!dirty || cfg.saving}
                >
                  Discard
                </Button>
                <Button
                  size="sm"
                  onClick={() => void cfg.handleSave()}
                  disabled={!dirty || cfg.saving}
                >
                  {cfg.saving ? 'Saving…' : 'Save changes'}
                </Button>
              </>
            )}
            <Button variant="outline" size="sm" onClick={handleClose} disabled={cfg.saving}>
              Close
            </Button>
          </Stack>
        </ModalFooter>
      </Modal>

      <ConfirmDialog
        open={discardOpen}
        onClose={() => setDiscardOpen(false)}
        title="Discard unsaved changes?"
        description="Your edits to this agent have not been saved and will be lost."
        confirmLabel="Discard changes"
        onConfirm={onClose}
      />

      <ConfirmDialog
        open={cfg.deleteOpen}
        onClose={() => cfg.setDeleteOpen(false)}
        title="Delete agent"
        description={`Permanently delete "${agent.name}"? All associated runs will be terminated. This cannot be undone.`}
        confirmLabel="Delete agent"
        onConfirm={() => void cfg.handleDelete()}
        confirming={cfg.deleting}
        error={cfg.deleteError}
      />
    </>
  );
}
