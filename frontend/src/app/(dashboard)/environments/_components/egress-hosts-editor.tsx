'use client';

/** @tier 3 — agents/environments-shared. Add/remove chip editor for an egress
 * host list, with an optional one-click "common package registries" quick-add.
 * Used by the Environments wizard and the agent Connectivity tab. */

import { useState } from 'react';
import { Button, Card, Icon, Input, Stack, Text } from '@orbit/hades';
import { COMMON_PACKAGE_REGISTRIES } from '../_types';

interface EgressHostsEditorProps {
  value: string[];
  onChange: (hosts: string[]) => void;
  /**
   * DOM id for the host input. Defaults to the Environments id; pass a distinct
   * value when more than one editor can appear on a page (e.g. the agent
   * Connectivity tab) to avoid duplicate ids.
   */
  inputId?: string;
  /**
   * When false, hides the "Add common package registries" quick-add. Defaults to
   * true (the Environments use case). The agent Connectivity tab opts out.
   */
  showCommonRegistries?: boolean;
}

export function EgressHostsEditor({
  value,
  onChange,
  inputId = 'environment-host-input',
  showCommonRegistries = true,
}: EgressHostsEditorProps) {
  const [draft, setDraft] = useState('');

  const addHost = (raw: string) => {
    const host = raw.trim();
    if (!host) return;
    if (value.includes(host)) {
      setDraft('');
      return;
    }
    onChange([...value, host]);
    setDraft('');
  };

  const removeHost = (host: string) => {
    onChange(value.filter((h) => h !== host));
  };

  // Append the curated registry list, deduped against the current hosts.
  const addCommonRegistries = () => {
    const next = [...value];
    for (const host of COMMON_PACKAGE_REGISTRIES) {
      if (!next.includes(host)) next.push(host);
    }
    onChange(next);
  };

  return (
    <Stack gap="snug">
      <Stack direction="horizontal" gap="snug" align="center">
        <Stack grow>
          <Input
            id={inputId}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault();
                addHost(draft);
              }
            }}
            placeholder="e.g. api.github.com or *.internal.acme.com"
          />
        </Stack>
        <Button type="button" variant="outline" size="sm" onClick={() => addHost(draft)}>
          <Icon name="plus" size="xs" />
          Add host
        </Button>
      </Stack>

      <Stack direction="horizontal" justify="between" align="center" wrap gap="snug">
        <Text size="caption" color="muted">
          Exact (api.github.com), subdomain wildcard (*.github.com), or * for any host.
        </Text>
        {showCommonRegistries && (
          <Button type="button" variant="ghost" size="sm" onClick={addCommonRegistries}>
            <Icon name="library" size="xs" />
            Add common package registries
          </Button>
        )}
      </Stack>

      {value.length === 0 ? (
        <Text size="supporting" color="muted">
          No hosts added yet.
        </Text>
      ) : (
        <Card border="default" divide>
          {value.map((host) => (
            <Stack
              key={host}
              direction="horizontal"
              align="center"
              justify="between"
              gap="snug"
              paddingBlock="tight"
              paddingInline="snug"
            >
              <Text size="caption" mono>
                {host}
              </Text>
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                onClick={() => removeHost(host)}
                aria-label={`Remove ${host}`}
              >
                <Icon name="trash" size="2xs" color="subtle" />
              </Button>
            </Stack>
          ))}
        </Card>
      )}
    </Stack>
  );
}
