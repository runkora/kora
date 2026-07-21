'use client';

/** @tier 3 — agents-scoped. Modal to start a new run for an agent type. */

import { useCallback, useEffect, useState } from 'react';
import {
  Alert,
  Button,
  FormField,
  Icon,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  NativeSelect,
  Stack,
  Text,
  Textarea,
  Toggle,
} from '@orbit/terra';
import { fetchStartRun } from '../_services/agents-api';
import type { AgentTypeRow } from '../_types';

interface StartRunModalProps {
  open: boolean;
  /** Pre-selected agent. When null and agentTypes is provided, user picks from a list. */
  agentType: AgentTypeRow | null;
  /** Full list of agents for the picker, used when no agentType is pre-selected. */
  agentTypes?: AgentTypeRow[];
  onClose: () => void;
  onStarted: (runId: string, agentSlug: string) => void;
}

export function StartRunModal({
  open,
  agentType,
  agentTypes,
  onClose,
  onStarted,
}: StartRunModalProps) {
  const [selectedAgentId, setSelectedAgentId] = useState('');
  const [task, setTask] = useState('');
  const [provisionSandbox, setProvisionSandbox] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const showPicker = !agentType && !!agentTypes?.length;
  const resolvedAgent = agentType ?? agentTypes?.find((a) => a.id === selectedAgentId) ?? null;

  // Reset form when modal opens/closes.
  useEffect(() => {
    if (!open) {
      setSelectedAgentId('');
      setTask('');
      setProvisionSandbox(false);
      setError(null);
    }
  }, [open]);

  const handleSubmit = useCallback(async () => {
    if (!resolvedAgent) return;
    const trimmedTask = task.trim();
    if (!trimmedTask) {
      setError('Task description is required.');
      return;
    }

    setError(null);
    setSubmitting(true);
    try {
      const result = await fetchStartRun(resolvedAgent.id, {
        task: trimmedTask,
        provision_sandbox: provisionSandbox,
      });
      if (result.error) {
        setError(result.error);
        return;
      }
      if (result.run) {
        onStarted(result.run.id, resolvedAgent.slug);
      }
    } catch {
      setError('Something went wrong. Please try again.');
    } finally {
      setSubmitting(false);
    }
  }, [resolvedAgent, task, provisionSandbox, onStarted]);

  return (
    <Modal open={open} onClose={onClose} size="md">
      <ModalHeader title={agentType ? `Start run: ${agentType.name}` : 'Start New Run'} />
      <ModalBody>
        <Stack gap="comfortable">
          {showPicker && (
            <FormField id="run-agent" label="Agent" required>
              <NativeSelect
                id="run-agent"
                value={selectedAgentId}
                onChange={(e) => setSelectedAgentId(e.target.value)}
              >
                <option value="" disabled>
                  Select an agent…
                </option>
                {agentTypes!
                  .filter((a) => a.state === 'active')
                  .map((a) => (
                    <option key={a.id} value={a.id}>
                      {a.name}
                    </option>
                  ))}
              </NativeSelect>
            </FormField>
          )}
          <FormField id="run-task" label="Task" hint="Describe what the agent should do." required>
            <Textarea
              id="run-task"
              value={task}
              onChange={(e) => setTask(e.target.value)}
              placeholder="e.g. Review the auth module for security issues..."
              rows={4}
            />
          </FormField>

          <Stack direction="horizontal" align="center" gap="snug">
            <Toggle
              checked={provisionSandbox}
              onChange={setProvisionSandbox}
              aria-labelledby="provision-sandbox-label"
            />
            <Stack gap="tight">
              <Text id="provision-sandbox-label" size="supporting" weight="emphasis">
                Pre-provision sandbox
              </Text>
              <Text size="supporting" color="muted">
                Reserve a sandbox pod upfront to reduce latency on first tool use.
              </Text>
            </Stack>
          </Stack>

          {error && <Alert variant="error">{error}</Alert>}
        </Stack>
      </ModalBody>
      <ModalFooter>
        <Button variant="outline" size="sm" onClick={onClose} disabled={submitting}>
          Cancel
        </Button>
        <Button
          variant="default"
          size="sm"
          onClick={() => void handleSubmit()}
          disabled={submitting || (showPicker && !selectedAgentId)}
        >
          <Icon name="play" size="xs" />
          {submitting ? 'Starting…' : 'Start Run'}
        </Button>
      </ModalFooter>
    </Modal>
  );
}
