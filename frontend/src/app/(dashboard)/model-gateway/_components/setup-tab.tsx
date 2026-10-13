'use client';

// ── SetupTab ────────────────────────────────────────────────────────────────
//
// Guides users through configuring a local LLM coding agent to route traffic
// through the kora Model Gateway.
//
// Structure:
//   1. Shared intro — how to self-service provision an API key
//   2. Agent-specific tabs — Claude Code, Codex CLI, OpenCode, VS Code

import { useSyncExternalStore } from 'react';
import Link from 'next/link';
import {
  Anchor,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  CardDescription,
  Code,
  CodeBlock,
  Heading,
  Stack,
  StepDot,
  StepConnector,
  Tabs,
  TabsList,
  TabsTrigger,
  TabsContent,
  Text,
  Callout,
} from '@orbit/hades';
import { ScopeChip } from '@orbit/components';
import { GATEWAY_BASE_URL_FALLBACK, gatewayBaseUrlFromWindow } from '../_utils';

// ── Step ─────────────────────────────────────────────────────────────────────

function Step({
  number,
  title,
  isLast,
  children,
}: {
  number: number;
  title: string;
  isLast?: boolean;
  children: React.ReactNode;
}) {
  return (
    <Stack direction="horizontal" gap="comfortable">
      <Stack align="center" gap="none">
        <StepDot>{number}</StepDot>
        {!isLast && <StepConnector />}
      </Stack>
      <Stack grow paddingBottom={isLast ? 'none' : 'spacious'}>
        <Stack marginBottom="tight">
          <Text size="supporting" weight="emphasis">
            {title}
          </Text>
        </Stack>
        <Stack gap="snug">{children}</Stack>
      </Stack>
    </Stack>
  );
}

// ── Agent tab content ─────────────────────────────────────────────────────────

function ClaudeCodeGuide({ baseUrl }: { baseUrl: string }) {
  return (
    <Stack gap="none">
      <Step number={1} title="Set the gateway endpoint">
        <Text size="supporting" color="muted">
          Claude Code reads <Code>ANTHROPIC_BASE_URL</Code> to override the default API endpoint.
          Add the following to your shell profile alongside the key you exported above.
        </Text>
        <CodeBlock
          text={`export ANTHROPIC_BASE_URL="${baseUrl}"
export ANTHROPIC_API_KEY="$ORBIT_API_KEY"`}
          filename="~/.zshrc"
        />
      </Step>

      <Step number={2} title="Map model aliases to your virtual models" isLast>
        <Text size="supporting" color="muted">
          Claude Code uses <Code>ANTHROPIC_DEFAULT_*_MODEL</Code> environment variables to select
          models by tier. Set each to the virtual model ID provisioned for you in kora.
        </Text>
        <CodeBlock
          text={`export ANTHROPIC_DEFAULT_SONNET_MODEL=anthropic/claude-sonnet-4-6
export ANTHROPIC_DEFAULT_HAIKU_MODEL=anthropic/claude-haiku-4-5
export ANTHROPIC_DEFAULT_OPUS_MODEL=anthropic/claude-opus-4-7
export CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY=1`}
          filename="~/.zshrc"
        />
        <Text size="caption" color="subtle">
          Then reload: <Code>source ~/.zshrc</Code>
        </Text>
      </Step>
    </Stack>
  );
}

function CodexGuide({ baseUrl }: { baseUrl: string }) {
  return (
    <Stack gap="none">
      <Step number={1} title="Set the active provider and model">
        <Text size="supporting" color="muted">
          Tell Codex to use kora and select your default virtual model by adding the following to
          your Codex config file.
        </Text>
        <CodeBlock
          text={`model_provider = "orbit"
model = "anthropic/claude-sonnet-4-6"`}
          filename="~/.codex/config.toml"
        />
      </Step>

      <Step number={2} title="Register the kora provider" isLast>
        <Text size="supporting" color="muted">
          Add the provider definition that Codex will look up when{' '}
          <Code>model_provider = &quot;orbit&quot;</Code> is set.
        </Text>
        <CodeBlock
          text={`[model_providers.orbit]
name     = "kora"
base_url = "${baseUrl}/v1"
env_key  = "ORBIT_API_KEY"
wire_api = "responses"`}
          filename="~/.codex/config.toml"
        />
        <Callout>
          <Stack gap="snug">
            <Heading variant="label">
              <Code>env_key</Code> is the name of an environment variable
            </Heading>
            <Text size="caption" color="muted">
              Codex reads the bearer token from the environment variable named by{' '}
              <Code>env_key</Code>. Export your kora API key as <Code>ORBIT_API_KEY</Code> in your
              shell profile.
            </Text>
          </Stack>
        </Callout>
        <Callout>
          <Stack gap="snug">
            <Heading variant="label">
              <Code>base_url</Code> must include the <Code>/v1</Code> suffix
            </Heading>
            <Text size="caption" color="muted">
              Omitting <Code>/v1</Code> causes all requests to return 403. Codex does not append it
              automatically.
            </Text>
          </Stack>
        </Callout>
      </Step>
    </Stack>
  );
}

function OpenCodeGuide({ baseUrl }: { baseUrl: string }) {
  return (
    <Stack gap="none">
      <Step number={1} title="Add kora as a provider" isLast>
        <Text size="supporting" color="muted">
          OpenCode supports custom OpenAI-compatible providers via its config file. Add the
          following to register kora and expose your virtual models.
        </Text>
        <CodeBlock
          text={`{
  "$schema": "https://opencode.ai/config.json",
  "provider": {
    "orbit": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "kora",
      "options": {
        "baseURL": "${baseUrl}/v1",
        "apiKey": "{env:ORBIT_API_KEY}"
      },
      "models": {
        "anthropic/claude-sonnet-4-6": {
          "name": "Claude Sonnet 4.6"
        },
        "anthropic/claude-opus-4-7": {
          "name": "Claude Opus 4.7"
        },
        "anthropic/claude-haiku-4-5": {
          "name": "Claude Haiku 4.5"
        }
      }
    }
  }
}`}
          filename="~/.config/opencode/opencode.jsonc"
        />
        <Callout>
          <Stack gap="snug">
            <Heading variant="label">
              <Code>{'{env:ORBIT_API_KEY}'}</Code> reads from your environment
            </Heading>
            <Text size="caption" color="muted">
              OpenCode resolves <Code>{'{env:VAR_NAME}'}</Code> at runtime from the named
              environment variable. Export <Code>ORBIT_API_KEY</Code> in your shell profile; no{' '}
              <Code>/connect</Code> command is needed when the key is supplied this way.
            </Text>
          </Stack>
        </Callout>
      </Step>
    </Stack>
  );
}

function VSCodeGuide({ baseUrl }: { baseUrl: string }) {
  return (
    <Stack gap="none">
      <Step number={1} title="Export the key where VS Code can see it">
        <Text size="supporting" color="muted">
          VS Code inherits environment variables from the shell that launched it. Export your key in
          your shell profile, then start VS Code from that shell with <Code>code .</Code> so the
          value is picked up.
        </Text>
        <CodeBlock
          text={`export ORBIT_API_KEY="orb_xxxxxxxx-xxxxxxxx-xxxxxxxx"
export ANTHROPIC_BASE_URL="${baseUrl}"
export ANTHROPIC_API_KEY="$ORBIT_API_KEY"`}
          filename="~/.zshrc"
        />
        <Text size="caption" color="subtle">
          Then reload: <Code>source ~/.zshrc</Code>
        </Text>
      </Step>

      <Step number={2} title="Point the Claude Code extension at the gateway">
        <Text size="supporting" color="muted">
          The Claude Code extension runs the same CLI underneath, so it reads the same variables.
          Pin them in your workspace settings if you prefer not to rely on the launching shell.
        </Text>
        <CodeBlock
          text={`{
  "claudeCode.environmentVariables": [
    { "name": "ANTHROPIC_BASE_URL", "value": "${baseUrl}" },
    { "name": "ANTHROPIC_DEFAULT_SONNET_MODEL", "value": "anthropic/claude-sonnet-4-6" },
    { "name": "ANTHROPIC_DEFAULT_HAIKU_MODEL", "value": "anthropic/claude-haiku-4-5" },
    { "name": "ANTHROPIC_DEFAULT_OPUS_MODEL", "value": "anthropic/claude-opus-4-7" },
    { "name": "CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY", "value": "1" }
  ]
}`}
          filename=".vscode/settings.json"
        />
        <Callout>
          <Stack gap="snug">
            <Heading variant="label">Keep the key out of settings files</Heading>
            <Text size="caption" color="muted">
              <Code>.vscode/settings.json</Code> is commonly committed. Set{' '}
              <Code>ANTHROPIC_API_KEY</Code> in your shell profile only, never here.
            </Text>
          </Stack>
        </Callout>
      </Step>

      <Step number={3} title="Or register kora as an OpenAI-compatible provider" isLast>
        <Text size="supporting" color="muted">
          Extensions that accept a custom OpenAI-compatible endpoint (Continue, Cline, Roo Code and
          similar) take the gateway directly. Point them at the <Code>/v1</Code> base URL and your
          virtual model IDs.
        </Text>
        <CodeBlock
          text={`{
  "models": [
    {
      "title": "kora — Claude Sonnet 4.6",
      "provider": "openai",
      "model": "anthropic/claude-sonnet-4-6",
      "apiBase": "${baseUrl}/v1",
      "apiKey": "\${env:ORBIT_API_KEY}"
    }
  ]
}`}
          filename="~/.continue/config.json"
        />
        <Callout>
          <Stack gap="snug">
            <Heading variant="label">
              <Code>apiBase</Code> must include the <Code>/v1</Code> suffix
            </Heading>
            <Text size="caption" color="muted">
              The OpenAI-compatible surface is served under <Code>/v1</Code>. Omitting it causes
              every request to return 403.
            </Text>
          </Stack>
        </Callout>
      </Step>
    </Stack>
  );
}

// ── SetupTab (exported) ──────────────────────────────────────────────────────

// useSyncExternalStore plumbing for reading the browser host: the host never
// changes, so the store has no subscription. Hydration uses the server
// snapshot (the placeholder), so the snippets match the SSR markup; React then
// re-renders with the real host after mount.
const subscribeNoop = () => () => {};
const getServerBaseUrl = () => GATEWAY_BASE_URL_FALLBACK;

export function SetupTab() {
  const baseUrl = useSyncExternalStore(subscribeNoop, gatewayBaseUrlFromWindow, getServerBaseUrl);

  return (
    <Stack gap="spacious">
      {/* ── Shared: provision an API key ─────────────────────────────────── */}
      <Card column toggleable defaultOpen={false}>
        <CardHeader>
          <CardTitle>Provision an API key</CardTitle>
          <CardDescription>
            All agents authenticate to the kora Model Gateway using a personal API key. Create one
            now and export it to your environment before continuing.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Stack>
            <Step number={1} title="Open the API Keys tab">
              <Text size="supporting" color="muted">
                Open the{' '}
                <Anchor asChild>
                  <Link href="/model-gateway/keys">API Keys</Link>
                </Anchor>{' '}
                tab and click{' '}
                <Text as="span" size="supporting" color="foreground" weight="emphasis">
                  Create key
                </Text>
                . Give it a descriptive name, for example the machine or agent it will be used on.
              </Text>
            </Step>

            <Step number={2} title="Select scopes">
              <Text size="supporting" color="muted">
                Scopes control what the key is permitted to do. Select the scopes your agent needs:
              </Text>
              <Stack gap="snug">
                <Stack direction="horizontal" gap="snug" align="center">
                  <ScopeChip scope="inference:invoke" />
                  <Text size="caption" color="muted">
                    Send inference requests to the gateway.
                  </Text>
                </Stack>
                <Stack direction="horizontal" gap="snug" align="center">
                  <ScopeChip scope="inference:list" />
                  <Text size="caption" color="muted">
                    List available models.
                  </Text>
                </Stack>
                <Stack direction="horizontal" gap="snug" align="center">
                  <ScopeChip scope="inference:anthropic:use" />
                  <Text size="caption" color="muted">
                    Route requests to Anthropic models specifically.
                  </Text>
                </Stack>
              </Stack>
              <Text size="caption" color="subtle">
                You can only grant scopes that have been assigned to your account. Contact your
                administrator if a required scope is unavailable.
              </Text>
            </Step>

            <Step number={3} title="Copy your key">
              <Text size="supporting" color="muted">
                After creation, copy the key immediately. It will only be shown once.
              </Text>
            </Step>

            <Step number={4} title="Export the key to your environment" isLast>
              <Text size="supporting" color="muted">
                Store the key in your shell profile. Never hard-code it in source files or commit it
                to version control.
              </Text>
              <CodeBlock
                text={`export ORBIT_API_KEY="orb_xxxxxxxx-xxxxxxxx-xxxxxxxx"`}
                filename="~/.zshrc"
              />
              <Text size="caption" color="subtle">
                Then reload your shell: <Code>source ~/.zshrc</Code>
              </Text>
            </Step>
          </Stack>
        </CardContent>
      </Card>

      {/* ── Agent-specific configuration ─────────────────────────────────── */}
      <Card column>
        <CardHeader>
          <CardTitle>Configure your agent</CardTitle>
          <CardDescription>
            Select your LLM coding agent below and follow the steps to point it at the kora Model
            Gateway.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Tabs defaultValue="claude-code">
            <TabsList>
              <TabsTrigger value="claude-code">Claude Code</TabsTrigger>
              <TabsTrigger value="codex">Codex CLI</TabsTrigger>
              <TabsTrigger value="opencode">OpenCode</TabsTrigger>
              <TabsTrigger value="vscode">VS Code</TabsTrigger>
            </TabsList>

            <TabsContent value="claude-code">
              <ClaudeCodeGuide baseUrl={baseUrl} />
            </TabsContent>

            <TabsContent value="codex">
              <CodexGuide baseUrl={baseUrl} />
            </TabsContent>

            <TabsContent value="opencode">
              <OpenCodeGuide baseUrl={baseUrl} />
            </TabsContent>

            <TabsContent value="vscode">
              <VSCodeGuide baseUrl={baseUrl} />
            </TabsContent>
          </Tabs>
        </CardContent>
      </Card>
    </Stack>
  );
}
