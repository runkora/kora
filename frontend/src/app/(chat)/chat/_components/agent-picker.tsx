'use client';

/**
 * @tier 3 — chat-scoped. Chooses the agent a new run starts: a button in the composer that opens
 * the agents the viewer can see, each with what it does and who its runs act as.
 */

import { useRef, useState } from 'react';
import {
  Button,
  Combobox,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
  Icon,
  Popover,
  PopoverContent,
  PopoverTrigger,
  Stack,
  Text,
} from '@runkora/hades';
import type { AgentTypeRow as Agent } from '@runkora/app/(dashboard)/agents/_types';

/** A list this long gets a search field. */
const SEARCH_FROM = 8;

/** Who a run of the agent acts as, in a few words. */
const actsAs = (agent: Agent) =>
  agent.identityMode === 'delegated' ? 'Acts as you' : 'Acts as itself';

function AgentOption({ agent, selected }: { agent: Agent; selected: boolean }) {
  return (
    <Stack
      direction="horizontal"
      align="center"
      gap="cozy"
      grow
      minWidth="none"
      paddingInline="tight"
    >
      <Icon
        name={selected ? 'check' : 'bot'}
        size="2xs"
        color={selected ? 'accent' : 'subtle'}
        label=""
      />
      <Stack gap="tight" grow minWidth="none">
        <Stack direction="horizontal" align="center" justify="between" gap="snug">
          <Text as="span" size="supporting" weight="strong">
            {agent.name}
          </Text>
          <Text as="span" size="caption" color="subtle">
            {agent.canRun ? actsAs(agent) : 'No access'}
          </Text>
        </Stack>
        <Text size="caption" color="muted" truncate title={agent.description}>
          {agent.description}
        </Text>
      </Stack>
    </Stack>
  );
}

export function AgentPicker({
  agents,
  value,
  onChange,
}: {
  agents: Agent[];
  value: Agent | null;
  onChange: (agent: Agent) => void;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const list = useRef<HTMLDivElement>(null);
  const search = agents.length >= SEARCH_FROM;
  const label = value?.name ?? 'Choose an agent';
  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) setQuery('');
      }}
    >
      <PopoverTrigger asChild>
        <Button variant="ghost" size="sm" aria-label={`Agent: ${label}`}>
          <Icon name="bot" size="2xs" label="" />
          {label}
          <Icon name="chevron-down" size="2xs" color="subtle" label="" />
        </Button>
      </PopoverTrigger>
      <PopoverContent
        wide
        align="start"
        onOpenAutoFocus={(e) => {
          // Without a search field, focus the list so the arrow keys move through it.
          if (search) return;
          e.preventDefault();
          list.current?.focus();
        }}
      >
        <Combobox ref={list} label="Agents" defaultValue={value?.slug}>
          {search && (
            <ComboboxInput
              placeholder="Search agents"
              aria-label="Search agents"
              value={query}
              onValueChange={setQuery}
            />
          )}
          <ComboboxList>
            {agents.map((a) => (
              <ComboboxItem
                key={a.id}
                value={a.slug}
                keywords={[a.name, a.description]}
                disabled={!a.canRun}
                onSelect={() => {
                  onChange(a);
                  setOpen(false);
                  setQuery('');
                }}
              >
                <AgentOption agent={a} selected={a.id === value?.id} />
              </ComboboxItem>
            ))}
            <ComboboxEmpty>No agent matches “{query.trim()}”.</ComboboxEmpty>
          </ComboboxList>
        </Combobox>
      </PopoverContent>
    </Popover>
  );
}
