/** @tier 3 — mcp-gateway-scoped. Multi-server "Build config" wizard: review the
 * grants, pick a client, copy the generated config. */
'use client';

import { useMemo, useState } from 'react';
import {
  Button,
  Card,
  CardContent,
  Eyebrow,
  Grid,
  Heading,
  Icon,
  Stack,
  Text,
  Wizard,
} from '@runkora/hades';
import {
  MCP_CLIENTS,
  PLATFORM_MCP_SERVER,
  buildMcpConfig,
  isCommandClient,
  type McpClient,
} from '@runkora/lib/mcp';
import { useBrand } from '@runkora/theme/brand-provider';
import type { McpServer } from '../_types';
import { serverRollupScope } from '../_types';
import { ServerAvatar } from './server-avatar';
import { ConfigBlock } from './config-block';

interface BuildConfigWizardProps {
  servers: McpServer[];
  selected: string[];
  onClose: () => void;
}

const STEPS = [
  { id: 'setup', title: 'Servers & client', sub: 'What you request and where' },
  { id: 'config', title: 'Config', sub: 'Copy and paste' },
];

export function BuildConfigWizard({ servers, selected, onClose }: BuildConfigWizardProps) {
  const { productName } = useBrand();
  const [step, setStep] = useState(0);
  const [maxVisited, setMaxVisited] = useState(0);
  const [client, setClient] = useState<McpClient>('desktop');

  const chosen = useMemo(
    () => servers.filter((s) => selected.includes(s.name)),
    [servers, selected],
  );
  const config = buildMcpConfig(client, selected);
  const clientDef = MCP_CLIENTS.find((c) => c.id === client) ?? MCP_CLIENTS[0];
  const isLast = step === STEPS.length - 1;

  const goTo = (next: number) => {
    setStep(next);
    setMaxVisited((m) => Math.max(m, next));
  };

  return (
    <Wizard
      open
      onClose={onClose}
      icon="terminal"
      title="Build config"
      subtitle={`${chosen.length} server${chosen.length === 1 ? '' : 's'} · one gateway endpoint`}
      rail={
        <Wizard.Rail
          label="Build config"
          steps={STEPS}
          current={step}
          maxVisited={maxVisited}
          onGoTo={setStep}
        />
      }
      footer={
        <Wizard.Footer
          step={step}
          totalSteps={STEPS.length}
          {...(step > 0 && { onBack: () => setStep((s) => s - 1) })}
          onNext={() => goTo(step + 1)}
          onSubmit={isLast ? onClose : undefined}
          submitLabel="Done"
        />
      }
    >
      <Wizard.Step>
        {step === 0 && (
          <>
            <Stack gap="snug">
              <Heading variant="subsection">Selected servers</Heading>
              <Text size="supporting" color="muted">
                This config will connect {chosen.length} MCP server{chosen.length === 1 ? '' : 's'}{' '}
                to your client through the {productName} gateway. Your client will only see the
                tools allowed by the grants each server requests below.
              </Text>
            </Stack>
            <Stack gap="snug">
              {chosen.map((s) => (
                <Card key={s.name} subtle>
                  <CardContent padded compact>
                    <Stack direction="horizontal" align="center" gap="snug">
                      <ServerAvatar
                        name={s.display_name}
                        accent={s.name === PLATFORM_MCP_SERVER}
                        size="xs"
                      />
                      <Stack grow gap="none">
                        <Text as="span" size="supporting" weight="strong">
                          {s.display_name}
                        </Text>
                        <Text as="span" mono size="micro" color="subtle">
                          Requests {serverRollupScope(s.name)}
                        </Text>
                      </Stack>
                      <Text as="span" size="micro" color="subtle">
                        {s.caller_identity === 'forward' ? 'Delegated' : 'Service'}
                      </Text>
                    </Stack>
                  </CardContent>
                </Card>
              ))}
            </Stack>
            <Stack gap="snug">
              <Heading variant="subsection">Choose a client</Heading>
              <Text size="supporting" color="muted">
                The config format and file location depend on where you will connect from.
              </Text>
            </Stack>
            <Grid cols={{ base: 2, sm: 4 }} gap="snug">
              {MCP_CLIENTS.map((c) => (
                <Button
                  key={c.id}
                  variant="outline"
                  selected={c.id === client}
                  aria-pressed={c.id === client}
                  onClick={() => setClient(c.id)}
                  // Icon above label. Button is a row flex by default, so the column
                  // direction and the tighter vertical padding are set here.
                  style={{
                    height: 'auto',
                    flexDirection: 'column',
                    gap: 6,
                    padding: '12px 8px',
                  }}
                >
                  <Icon name={c.icon} size="xl" />
                  <Text as="span" size="caption" weight="emphasis" color="foreground">
                    {c.label}
                  </Text>
                </Button>
              ))}
            </Grid>
          </>
        )}

        {isLast && (
          <>
            <Stack gap="snug">
              <Heading variant="subsection">Your config</Heading>
              <Text size="supporting" color="muted">
                Paste this into your {clientDef.label} setup.
              </Text>
            </Stack>
            <Stack gap="snug">
              <Stack direction="horizontal" align="center" justify="between">
                <Eyebrow>Config</Eyebrow>
                <Text as="span" mono size="micro" weight="emphasis" color="subtle">
                  {clientDef.file}
                </Text>
              </Stack>
              <ConfigBlock text={config} command={isCommandClient(client)} />
            </Stack>
            <Card subtle>
              <CardContent padded compact>
                <Stack direction="horizontal" gap="snug">
                  <Icon name="shield" color="accent" size="sm" label="" />
                  <Stack gap="tight">
                    <Text as="span" size="caption" weight="strong">
                      Authentication
                    </Text>
                    <Text as="span" size="caption" color="muted">
                      The first time you connect, a browser window opens so you can sign in. Agents
                      get a token automatically. This file contains no secrets.
                    </Text>
                  </Stack>
                </Stack>
              </CardContent>
            </Card>
          </>
        )}
      </Wizard.Step>
    </Wizard>
  );
}
