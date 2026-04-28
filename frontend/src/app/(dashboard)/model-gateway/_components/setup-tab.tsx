'use client';

// ── SetupTab ────────────────────────────────────────────────────────────────
//
// Guides users through configuring their local machine to route LLM traffic
// through the Orbit Model Gateway. Step-by-step instructions with copyable
// code blocks.
//
// API key management and usage/limits live in their own dedicated tabs
// (Keys and Overview respectively) — this tab is focused purely on the
// getting-started workflow.

import { useState, useCallback } from 'react';
import {
  Callout,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  CardDescription,
  Code,
  Heading,
  Separator,
  Icon,
  Stack,
  StepDot,
  StepConnector,
  Text,
} from '@orbit/terra';

// ── CodeBlock ────────────────────────────────────────────────────────────────

function CodeBlock({ code }: { code: string }) {
  const [copied, setCopied] = useState(false);

  const handleCopy = useCallback(() => {
    void navigator.clipboard.writeText(code).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  }, [code]);

  return (
    <Stack asChild>
      <figure className="group relative m-0">
        <pre className="overflow-x-auto rounded-lg bg-surface-elevated border border-border px-4 py-3 leading-relaxed">
          <Code variant="bare">{code}</Code>
        </pre>
        <Button variant="ghost" size="icon-sm" onClick={handleCopy} aria-label="Copy code">
          {copied ? (
            <Icon name="check" size="2xs" color="status-healthy" />
          ) : (
            <Icon name="copy" size="2xs" />
          )}
        </Button>
      </figure>
    </Stack>
  );
}

// ── Step ─────────────────────────────────────────────────────────────────────

function Step({
  number,
  title,
  children,
}: {
  number: number;
  title: string;
  children: React.ReactNode;
}) {
  return (
    <Stack direction="horizontal" gap="comfortable">
      <Stack align="center" gap="none">
        <StepDot>{number}</StepDot>
        <StepConnector />
      </Stack>
      <Stack grow paddingBottom="spacious">
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

// ── SetupTab (exported) ──────────────────────────────────────────────────────

export function SetupTab() {
  return (
    <Stack gap="spacious">
      <Card column>
        <CardHeader>
          <CardTitle>Getting started</CardTitle>
          <CardDescription>
            Configure your local coding agent to route requests through the Orbit Model Gateway in
            three steps.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Stack>
            <Step number={1} title="Create an API key">
              <Text size="supporting" color="muted">
                Head to the{' '}
                <Text as="span" size="supporting" color="foreground" weight="emphasis">
                  API Keys
                </Text>{' '}
                tab to create a key, then store it in your environment — never hard-code it in
                source files.
              </Text>
              <CodeBlock
                code={`# ~/.zshrc or ~/.bashrc
export ORBIT_API_KEY="sk-orbit-xxxxxxxx-xxxxxxxx-xxxxxxxx"`}
              />
              <Text size="caption" color="subtle">
                Then reload your shell: <Code>source ~/.zshrc</Code>
              </Text>
            </Step>

            <Step number={2} title="Point your agent at the gateway">
              <Text size="supporting" color="muted">
                Set the base URL to the Orbit Model Gateway endpoint. Most OpenAI-compatible clients
                accept a <Code>base_url</Code> or <Code>OPENAI_BASE_URL</Code> environment variable.
              </Text>
              <CodeBlock
                code={`export OPENAI_BASE_URL="https://gateway.orbit.dev/v1"
export OPENAI_API_KEY="$ORBIT_API_KEY"`}
              />
            </Step>

            <Step number={3} title="Verify the connection">
              <Text size="supporting" color="muted">
                Send a quick test request to confirm everything is wired up correctly.
              </Text>
              <CodeBlock
                code={`curl "$OPENAI_BASE_URL/models" \\
  -H "Authorization: Bearer $ORBIT_API_KEY" | jq '.data[].id'`}
              />
              <Text size="supporting" color="muted">
                Or with the Python SDK:
              </Text>
              <CodeBlock
                code={`import openai, os

client = openai.OpenAI(
    api_key=os.environ["ORBIT_API_KEY"],
    base_url=os.environ["OPENAI_BASE_URL"],
)

response = client.chat.completions.create(
    model="claude-3-5-sonnet-20241022",
    messages=[{"role": "user", "content": "Hello from Orbit!"}],
)
print(response.choices[0].message.content)`}
              />
            </Step>
          </Stack>

          <Stack asChild>
            <figure className="m-0 my-4">
              <Separator span="full" />
            </figure>
          </Stack>

          <Callout>
            <Stack gap="snug">
              <Heading variant="label">Compatible clients</Heading>
              <Text size="caption" color="muted">
                Any OpenAI-compatible client works with the Orbit gateway — including Cursor, Aider,
                Continue, Cline, and custom scripts using the OpenAI Python or Node.js SDKs.
              </Text>
            </Stack>
          </Callout>
        </CardContent>
      </Card>
    </Stack>
  );
}
