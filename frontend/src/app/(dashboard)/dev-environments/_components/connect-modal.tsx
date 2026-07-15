'use client';

/** @tier 3 — dev-environments-scoped. Brokers a short-lived kubeconfig for one
 * dev environment and shows it once — the private key never leaves the
 * identity service and this response is never persisted client-side. */

import { useEffect, useState } from 'react';
import {
  Alert,
  Callout,
  Code,
  CodeBlock,
  Icon,
  Modal,
  ModalAccessibleTitle,
  ModalBody,
  ModalFooter,
  Button,
  Stack,
  Text,
} from '@orbit/terra';
import type { DevEnvironment, DevEnvironmentKubeconfigResponse } from '@orbit/shared/api';
import { formatTimestamp } from '../_types';

interface ConnectModalProps {
  environment: DevEnvironment | null;
  onClose: () => void;
  onFetchKubeconfig: (
    id: string,
  ) => Promise<{ kubeconfig: DevEnvironmentKubeconfigResponse | null; error: string | null }>;
  onWake: (id: string) => Promise<{ error: string | null }>;
}

export function ConnectModal({
  environment,
  onClose,
  onFetchKubeconfig,
  onWake,
}: ConnectModalProps) {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<DevEnvironmentKubeconfigResponse | null>(null);
  const [waking, setWaking] = useState(false);

  const envId = environment?.id;

  useEffect(() => {
    if (!envId) {
      setResult(null);
      setError(null);
      return;
    }
    setLoading(true);
    setError(null);
    void onFetchKubeconfig(envId).then((res) => {
      setLoading(false);
      if (res.error || !res.kubeconfig) {
        setError(res.error ?? 'Failed to broker a kubeconfig.');
        return;
      }
      setResult(res.kubeconfig);
    });
    // Only re-fetch when the target environment id changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [envId]);

  const handleWake = async () => {
    if (!envId) return;
    setWaking(true);
    await onWake(envId);
    setWaking(false);
  };

  return (
    <Modal open={!!environment} onClose={onClose} size="lg">
      {/* No ModalHeader — custom layout below. ModalAccessibleTitle satisfies
          the Radix DialogTitle requirement for screen readers. */}
      <ModalAccessibleTitle>
        Connect to {environment?.ref || 'dev environment'}
      </ModalAccessibleTitle>
      <ModalBody flushY>
        <Stack gap="comfortable">
          <Stack gap="tight">
            <Text size="body" weight="heavy" as="span">
              Connect to {environment?.ref || 'dev environment'}
            </Text>
            <Text size="supporting" color="muted">
              A short-lived kubeconfig, scoped to this environment&apos;s vcluster. You are
              cluster-admin inside it — nothing you do there is visible to the host.
            </Text>
          </Stack>

          {environment?.phase === 'Sleeping' && (
            <Callout variant="warning">
              <Stack direction="horizontal" align="center" justify="between" gap="comfortable">
                <Text size="supporting" color="foreground">
                  This environment is asleep. <Code>kubectl</Code> will fail until it&apos;s woken.
                </Text>
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => void handleWake()}
                  disabled={waking}
                >
                  <Icon name="sun" size="xs" />
                  {waking ? 'Waking…' : 'Wake now'}
                </Button>
              </Stack>
            </Callout>
          )}

          {loading && (
            <Text size="supporting" color="muted">
              Signing a client certificate against the vcluster&apos;s own CA…
            </Text>
          )}
          {error && <Alert variant="error">{error}</Alert>}
          {result && (
            <Stack gap="tight">
              <CodeBlock text={result.kubeconfig} filename="kubeconfig.yaml" wrap />
              <Text size="caption" color="subtle">
                Save this now — it is shown only once. Expires {formatTimestamp(result.expiresAt)}.
              </Text>
            </Stack>
          )}
        </Stack>
      </ModalBody>
      <ModalFooter>
        <Button size="sm" onClick={onClose}>
          Done
        </Button>
      </ModalFooter>
    </Modal>
  );
}
