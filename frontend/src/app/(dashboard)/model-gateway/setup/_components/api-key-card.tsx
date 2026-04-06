'use client';

import { useState, useCallback } from 'react';
import { Copy, Check, RefreshCw } from 'lucide-react';
import {
  Button,
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
  CardDescription,
  Stack,
  Text,
} from '@orbit/terra';

// ── Helpers ───────────────────────────────────────────────────────────────────

/** Generates a random API key in the style: omg-<16 hex chars> */
function generateApiKey(): string {
  const chars = 'abcdef0123456789';
  const segment = (len: number) =>
    Array.from({ length: len }, () => chars[Math.floor(Math.random() * chars.length)]).join('');
  return `sk-orbit-${segment(8)}-${segment(8)}-${segment(8)}`;
}

/** Masks the key, revealing only the prefix and last 4 chars */
function maskKey(key: string): string {
  // Show first 12 chars (e.g. "sk-orbit-xxxx") + dots + last 4
  const visible = 12;
  const tail = 4;
  if (key.length <= visible + tail) return key;
  const prefix = key.slice(0, visible);
  const suffix = key.slice(-tail);
  const dots = '•'.repeat(Math.min(16, key.length - visible - tail));
  return `${prefix}${dots}${suffix}`;
}

// ── ApiKeyCard ────────────────────────────────────────────────────────────────

export function ApiKeyCard() {
  // Lazy initializer: Math.random() only runs on the client, satisfying the
  // linter (no setState inside an effect) while still producing a stable key.
  const [apiKey, setApiKey] = useState(generateApiKey);
  const [copied, setCopied] = useState(false);
  const [generating, setGenerating] = useState(false);

  const handleCopy = useCallback(() => {
    void navigator.clipboard.writeText(apiKey).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  }, [apiKey]);

  const handleGenerate = useCallback(() => {
    setGenerating(true);
    // Simulate async keygen
    setTimeout(() => {
      setApiKey(generateApiKey());
      setGenerating(false);
    }, 600);
  }, []);

  const masked = maskKey(apiKey);

  return (
    <Card column>
      <CardHeader>
        <CardTitle>API Key</CardTitle>
        <CardDescription>
          Use this key to authenticate requests to the Model Gateway from your local coding agent or
          scripts.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <Stack gap="snug">
          <Text size="caption" color="muted">
            Your API key is partially masked. Click the field or copy icon to copy the full key.
          </Text>

          {/* Masked key field */}
          <Stack asChild direction="horizontal" align="center" justify="between">
            <button
              type="button"
              className="group cursor-pointer rounded-md border border-border bg-surface-elevated px-3 py-2.5 transition-colors hover:border-border-strong w-full text-left"
              onClick={handleCopy}
              aria-label="Copy API key"
            >
              <span className="select-none min-w-0 block">
                <Text size="supporting" mono truncate>
                  {masked}
                </Text>
              </span>
              <span className="ml-3 shrink-0 text-muted-foreground transition-colors group-hover:text-foreground">
                {copied ? (
                  <Check className="size-4 text-status-healthy" />
                ) : (
                  <Copy className="size-4" />
                )}
              </span>
            </button>
          </Stack>

          {copied && (
            <Text size="caption" color="status-healthy">
              ✓ Copied to clipboard
            </Text>
          )}
        </Stack>
      </CardContent>
      <CardFooter justify="between">
        <Text size="caption" color="subtle">
          Treat this key like a password — never commit it to source control.
        </Text>
        <Button variant="outline" size="sm" onClick={handleGenerate} disabled={generating}>
          <RefreshCw className={['size-3.5', generating ? 'animate-spin' : ''].join(' ')} />
          {generating ? 'Generating…' : 'New key'}
        </Button>
      </CardFooter>
    </Card>
  );
}
