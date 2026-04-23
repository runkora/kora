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
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  CardDescription,
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
        <pre className="overflow-x-auto rounded-lg bg-surface-elevated border border-border px-4 py-3 text-xs leading-relaxed">
          <code className="font-mono text-foreground">{code}</code>
        </pre>
        <button
          type="button"
          onClick={handleCopy}
          aria-label="Copy code"
          className="absolute right-2 top-2 rounded p-1.5 text-muted-foreground opacity-0 transition-all hover:bg-surface hover:text-foreground group-hover:opacity-100 focus-visible:opacity-100 focus-visible:outline-2 focus-visible:outline-accent"
        >
          {copied ? (
            <Icon name="check" size="2xs" color="status-healthy" />
          ) : (
            <Icon name="copy" size="2xs" />
          )}
        </button>
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
      <section className="flex-1 pb-8">
        <header className="mb-2">
          <Text size="supporting" weight="emphasis">
            {title}
          </Text>
        </header>
        <Stack gap="snug">{children}</Stack>
      </section>
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
                Then reload your shell: <code className="font-mono text-xs">source ~/.zshrc</code>
              </Text>
            </Step>

            <Step number={2} title="Point your agent at the gateway">
              <Text size="supporting" color="muted">
                Set the base URL to the Orbit Model Gateway endpoint. Most OpenAI-compatible clients
                accept a{' '}
                <code className="rounded bg-surface-elevated px-1 font-mono text-xs">base_url</code>{' '}
                or{' '}
                <code className="rounded bg-surface-elevated px-1 font-mono text-xs">
                  OPENAI_BASE_URL
                </code>{' '}
                environment variable.
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

          <Stack asChild gap="snug">
            <aside className="rounded-lg bg-surface-elevated border border-border px-4 py-3">
              <header className="mb-1">
                <Heading variant="label">Compatible clients</Heading>
              </header>
              <Text size="caption" color="muted">
                Any OpenAI-compatible client works with the Orbit gateway — including Cursor, Aider,
                Continue, Cline, and custom scripts using the OpenAI Python or Node.js SDKs.
              </Text>
            </aside>
          </Stack>
        </CardContent>
      </Card>
    </Stack>
  );
}
