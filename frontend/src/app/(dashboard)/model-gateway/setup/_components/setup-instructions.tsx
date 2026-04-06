'use client';

import { useState, useCallback } from 'react';
import { Copy, Check } from 'lucide-react';
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  CardDescription,
  Heading,
  Stack,
  Text,
  Separator,
} from '@orbit/terra';

// ── CodeBlock ─────────────────────────────────────────────────────────────────

interface CodeBlockProps {
  code: string;
  language?: string;
}

function CodeBlock({ code, language }: CodeBlockProps) {
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
            <Check className="size-3.5 text-status-healthy" />
          ) : (
            <Copy className="size-3.5" />
          )}
        </button>
      </figure>
    </Stack>
  );
}

// ── Step ──────────────────────────────────────────────────────────────────────

interface StepProps {
  number: number;
  title: string;
  children: React.ReactNode;
}

function Step({ number, title, children }: StepProps) {
  return (
    <Stack direction="horizontal" gap="comfortable">
      <Stack align="center" gap="none">
        <Stack asChild align="center" justify="center">
          <span className="flex size-6 shrink-0 items-center justify-center rounded-full bg-accent text-[11px] font-bold text-white">
            {number}
          </span>
        </Stack>
        <span className="mt-2 w-px flex-1 bg-border block" />
      </Stack>
      <Stack asChild>
        <section className="flex-1 pb-8">
          <Stack asChild>
            <header className="mb-2">
              <Text size="supporting" weight="emphasis">
                {title}
              </Text>
            </header>
          </Stack>
          <Stack gap="tight">{children}</Stack>
        </section>
      </Stack>
    </Stack>
  );
}

// ── SetupInstructions ─────────────────────────────────────────────────────────

export function SetupInstructions() {
  return (
    <Card column>
      <CardHeader>
        <CardTitle>Setup instructions</CardTitle>
        <CardDescription>
          Configure your local coding agent to route requests through the Orbit Model Gateway in
          three steps.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <Stack>
          <Step number={1} title="Store your API key securely">
            <Text size="supporting" color="muted">
              Add your key to your environment — never hard-code it in source files.
            </Text>
            <CodeBlock
              language="bash"
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
              accept an{' '}
              <code className="rounded bg-surface-elevated px-1 font-mono text-xs">base_url</code>{' '}
              or{' '}
              <code className="rounded bg-surface-elevated px-1 font-mono text-xs">
                OPENAI_BASE_URL
              </code>{' '}
              environment variable.
            </Text>
            <CodeBlock
              language="bash"
              code={`export OPENAI_BASE_URL="https://gateway.orbit.dev/v1"
export OPENAI_API_KEY="$ORBIT_API_KEY"`}
            />
          </Step>

          <Step number={3} title="Verify the connection">
            <Text size="supporting" color="muted">
              Send a quick test request to confirm everything is wired up correctly.
            </Text>
            <CodeBlock
              language="bash"
              code={`curl "$OPENAI_BASE_URL/models" \\
  -H "Authorization: Bearer $ORBIT_API_KEY" | jq '.data[].id'`}
            />
            <Text size="supporting" color="muted">
              Or with the Python SDK:
            </Text>
            <CodeBlock
              language="python"
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
          <figure className="my-2 m-0">
            <Separator span="full" />
          </figure>
        </Stack>

        <Stack asChild gap="tight">
          <aside className="rounded-lg bg-surface-elevated border border-border px-4 py-3">
            <Stack asChild>
              <header className="mb-1">
                <Heading variant="label">Compatible clients</Heading>
              </header>
            </Stack>
            <Text size="caption" color="muted">
              Any OpenAI-compatible client works with the Orbit gateway — including Cursor, Aider,
              Continue, Cline, and custom scripts using the OpenAI Python or Node.js SDKs.
            </Text>
          </aside>
        </Stack>
      </CardContent>
    </Card>
  );
}
