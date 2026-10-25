'use client';

/** @tier 3 — agents/[slug]-scoped. The Configuration tab: the builder's rail +
 * section anatomy applied to editing, on the shared motif components. One
 * dirty state, one save, one re-provision
 * (docs/adr/2026-07-17-agent-detail-configuration.md). */

import { useState } from 'react';
import {
  Alert,
  Button,
  Card,
  CardContent,
  Eyebrow,
  FormField,
  Grid,
  Input,
  SaveBar,
  SectionHeading,
  Separator,
  Stack,
  Text,
  Textarea,
  ConfirmDialog,
} from '@runkora/hades';
import { useBrand } from '@runkora/theme/brand-provider';
import type { AgentFramework } from '@runkora/shared/api';
import { FrameworkCards } from '../../_components/motif/framework-cards';
import { IDENTITY_MODE_SUMMARIES } from '../../_components/motif/identity-mode-cards';
import { ModelPicker } from '@runkora/components';
import { McpToolTree, countGrantedServers } from '../../_components/motif/mcp-tool-tree';
import { ConnectionCards } from '@runkora/components';
import { EgressModeCards } from '../../_components/motif/egress-mode-cards';
import { EnvironmentsCard } from '../../_components/motif/environments-card';
import { GuardrailsFields } from '../../_components/motif/guardrail-rows';
import type { GuardrailValues } from '../../_components/motif/guardrail-rows';
import { PlannedLimits } from '../../_components/motif/planned-limits';
import {
  EffectiveAccessPanel,
  ManagedInGitNotice,
  PermissionHint,
  permissionHintText,
} from '@runkora/components';
import { managedInGit as isManagedInGit } from '@runkora/lib/declarative';
import { useTenantLocked } from '../../../_components/permissions-provider';
import { ConfigurationRail } from './configuration-rail';
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

interface AgentConfigurationTabProps {
  /** The rail step to open on, for a link from another tab. */
  initialSection?: string | undefined;
}

export function AgentConfigurationTab({ initialSection = 'identity' }: AgentConfigurationTabProps) {
  const cfg = useAgentConfiguration();
  const { productName } = useBrand();
  const [active, setActive] = useState(initialSection);
  // Capacity has a stored value on every live agent; "off" here means "leave
  // it as it is", so the row dims but nothing is unset on save.
  const [capacityOn, setCapacityOn] = useState(true);
  const locked = useTenantLocked();

  if (!cfg.agent) return null;
  const agent = cfg.agent;
  // Without edit access every field is read only; the notice above the form says why.
  const readOnly = !agent.canEdit;
  // A declared agent, or any agent in a locked tenant, changes only in the configuration repository.
  const managedInGit = isManagedInGit(agent.declared, locked);

  const guardrailValues: GuardrailValues = {
    agentBudgetOn: cfg.budgetLimitUSD !== '',
    agentBudgetUsd: cfg.budgetLimitUSD || '500.00',
    agentBudgetCadence: cfg.budgetCadence === 'weekly' ? 'weekly' : 'monthly',
    runBudgetOn: cfg.budgetPerRunUSD !== '',
    runBudgetUsd: cfg.budgetPerRunUSD || '5.00',
    capacityOn,
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
        setCapacityOn(Boolean(value));
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
    <Stack direction="horizontal" gap="relaxed" align="start">
      <ConfigurationRail active={active} onSelect={setActive} updatedAt={agent.updatedAt} />

      <Stack grow gap="relaxed">
        {managedInGit ? (
          <ManagedInGitNotice declared={agent.declared} repository={agent.repository} />
        ) : (
          readOnly && (
            <Alert variant="info">
              <Text size="caption">
                {permissionHintText('change this agent')} Changing it needs the agents:types:
                {agent.identityMode}:write scope
                {agent.identityMode === 'autonomous' ? ' and every scope the agent holds.' : '.'}
              </Text>
            </Alert>
          )
        )}
        {/* ── Identity ─────────────────────────────────────────────────────── */}
        {active === 'identity' && (
          <Stack gap="relaxed">
            <SectionHeading
              icon="user"
              title="Identity"
              description="Who this agent is on the control plane. The slug is its permanent identity and cannot change."
            />
            <Grid cols={2} gap="comfortable">
              <FormField label="Display name" required>
                <Input
                  disabled={readOnly}
                  value={cfg.name}
                  onChange={(e) => cfg.setName(e.target.value)}
                  maxLength={128}
                />
              </FormField>
              <FormField label="Slug" hint="Cannot be changed after creation.">
                <Input mono readOnly value={agent.slug} aria-label="Slug (immutable)" />
              </FormField>
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
            <FormField
              label="Identity model"
              hint="Chosen at creation and cannot be changed. Recreate the agent to use the other model."
            >
              <Input
                readOnly
                value={IDENTITY_MODE_SUMMARIES[agent.identityMode]}
                aria-label="Identity model (immutable)"
              />
            </FormField>
          </Stack>
        )}

        {/* ── Behaviour ────────────────────────────────────────────────────── */}
        {active === 'behaviour' && (
          <Stack gap="relaxed">
            <SectionHeading
              icon="bot"
              title="Behaviour"
              description="How the agent behaves, and the model and framework it runs on."
            />
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
                <Text as="span" size="micro" color="subtle">
                  {cfg.models.length} enabled by your platform
                </Text>
              </Stack>
              {cfg.modelsError && (
                <Alert variant="error">Models failed to load: {cfg.modelsError}</Alert>
              )}
              <ModelPicker
                disabled={readOnly}
                models={cfg.models}
                value={cfg.modelId}
                onChange={cfg.setModelId}
              />
            </Stack>
          </Stack>
        )}

        {/* ── Tools & access ───────────────────────────────────────────────── */}
        {active === 'tools' && (
          <Stack gap="relaxed">
            <SectionHeading
              icon="wrench"
              title="Tools & access"
              description="The MCP servers and connections this agent may call. Every call is checked before it runs."
            />
            <Text as="span" size="supporting" color="muted">
              <Text as="span" size="supporting" weight="heavy" color="accent">
                {countGrantedServers(cfg.mcpServers, cfg.mcpCatalog, cfg.mcpTools)}
              </Text>{' '}
              servers ·{' '}
              <Text as="span" size="supporting" weight="heavy">
                {cfg.mcpTools.length}
              </Text>{' '}
              tools granted
            </Text>
            {cfg.mcpError && (
              <Alert variant="error">MCP servers failed to load: {cfg.mcpError}</Alert>
            )}
            <McpToolTree
              disabled={readOnly}
              servers={cfg.mcpServers}
              catalog={cfg.mcpCatalog}
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
                available={cfg.availableConnections}
                value={cfg.connectors}
                onChange={cfg.setConnectors}
              />
            </Stack>
          </Stack>
        )}

        {/* ── Network access ───────────────────────────────────────────────── */}
        {active === 'network' && (
          <Stack gap="relaxed">
            <SectionHeading
              icon="network"
              title="Network access"
              description="The hosts the agent may reach, and the environments it can run in."
            />
            <Stack gap="cozy">
              <SectionLabel title="Network egress" desc="The hosts the agent may reach directly." />
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
        {active === 'guardrails' && (
          <Stack gap="relaxed">
            <SectionHeading
              icon="shield"
              title="Guardrails"
              description={`Set the ceilings ${productName} enforces at runtime: spend, capacity and loop limits.`}
            />
            <Stack direction="horizontal" align="baseline" gap="snug">
              <Eyebrow>Limits</Eyebrow>
              <Text as="span" size="micro" color="subtle">
                Every limit is optional. Switch on the ones you want {productName} to enforce.
              </Text>
            </Stack>
            <GuardrailsFields
              disabled={readOnly}
              values={guardrailValues}
              onChange={onGuardrailChange}
            />
            <PlannedLimits />
          </Stack>
        )}

        {/* ── Danger zone ──────────────────────────────────────────────────── */}
        {active === 'danger' && (
          <Stack gap="relaxed">
            <SectionHeading
              icon="triangle-alert"
              title="Danger zone"
              description="Destructive actions. These cannot be undone."
            />
            <Card border="default">
              <CardContent padded>
                <Stack direction="horizontal" align="center" gap="comfortable">
                  <Stack grow gap="tight">
                    <Text as="span" size="supporting" weight="heavy">
                      Delete this agent
                    </Text>
                    <Text size="caption" color="muted">
                      Deregisters the agent, tears down its workload, and terminates all associated
                      runs.
                    </Text>
                  </Stack>
                  {!managedInGit && (
                    <PermissionHint allowed={agent.canDelete} action="delete this agent">
                      <Button
                        variant="destructive"
                        disabled={!agent.canDelete}
                        onClick={() => cfg.setDeleteOpen(true)}
                      >
                        Delete agent
                      </Button>
                    </PermissionHint>
                  )}
                </Stack>
              </CardContent>
            </Card>
          </Stack>
        )}
      </Stack>

      <SaveBar
        visible={!readOnly && cfg.isDirty}
        saving={cfg.saving}
        success={cfg.saveSuccess}
        error={cfg.saveError}
        onSave={() => void cfg.handleSave()}
        onDiscard={cfg.handleDiscard}
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
    </Stack>
  );
}
