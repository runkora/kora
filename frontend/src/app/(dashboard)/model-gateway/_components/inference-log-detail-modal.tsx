'use client';

import {
  CopyField,
  Badge,
  Modal,
  ModalBody,
  ModalHeader,
  Separator,
  Stack,
  Text,
} from '@orbit/hades';
import type { InferenceUsageRecord } from '../_types';
import { fmtCost, fmtDate, fmtNum, fmtTime } from '@orbit/utils';
import { getInferenceStatusVariant, fmtInferenceStatus } from '../_utils';

// ── DetailRow ─────────────────────────────────────────────────────────────────

interface DetailRowProps {
  label: string;
  children: React.ReactNode;
}

function DetailRow({ label, children }: DetailRowProps) {
  return (
    <Stack
      direction="horizontal"
      align="center"
      justify="between"
      paddingBlock="tight"
      paddingInline="snug"
      gap="comfortable"
    >
      <Text size="caption" color="muted">
        {label}
      </Text>
      {children}
    </Stack>
  );
}

// ── InferenceLogDetailModal ───────────────────────────────────────────────────

interface InferenceLogDetailModalProps {
  record: InferenceUsageRecord | null;
  keyName?: string;
  onClose: () => void;
}

export function InferenceLogDetailModal({
  record,
  keyName,
  onClose,
}: InferenceLogDetailModalProps) {
  return (
    <Modal open={!!record} onClose={onClose} size="lg">
      {record && (
        <>
          <ModalHeader title="Request detail" description="Click any field to copy" />

          {/* Status strip */}
          <Stack
            direction="horizontal"
            align="center"
            gap="snug"
            paddingBlock="snug"
            paddingInline="comfortable"
          >
            <Badge
              variant={getInferenceStatusVariant(record.status)}
              size="sm"
              dot={record.status === 'success'}
            >
              {fmtInferenceStatus(record.status)}
            </Badge>
            <Text size="caption" color="subtle" mono>
              {fmtDate(record.created_at)} at {fmtTime(record.created_at)}
            </Text>
          </Stack>
          <Separator />

          <ModalBody flush gapless constrained>
            {/* Identity */}
            <Stack padding="snug">
              <Stack paddingTop="none" paddingInline="snug" paddingBottom="tight">
                <Text size="caption" color="muted">
                  Identity
                </Text>
              </Stack>
              <CopyField label="Request ID" value={record.id} mono wrap />
              <CopyField label="Principal ID" value={record.principal_id} mono wrap />
              <DetailRow label="Principal type">
                <Text size="supporting" mono>
                  {record.principal_type}
                </Text>
              </DetailRow>
              {record.token_id && (
                <CopyField label="API key ID" value={record.token_id} mono wrap />
              )}
              {keyName && <CopyField label="API key name" value={keyName} wrap />}
              <DetailRow label="API dialect">
                <Text size="supporting" mono>
                  {record.api_dialect}
                </Text>
              </DetailRow>
            </Stack>

            <Separator />

            {/* Model */}
            <Stack padding="snug">
              <Stack paddingTop="none" paddingInline="snug" paddingBottom="tight">
                <Text size="caption" color="muted">
                  Model
                </Text>
              </Stack>
              <CopyField
                label="Virtual model ID"
                value={record.virtual_model_id ?? record.model_id}
                mono
                wrap
              />
              {record.virtual_model_id && record.virtual_model_id !== record.model_id && (
                <CopyField label="Provider model ID" value={record.model_id} mono wrap />
              )}
              {record.provider && (
                <DetailRow label="Provider">
                  <Text size="supporting" mono>
                    {record.provider}
                  </Text>
                </DetailRow>
              )}
              {record.model_family && (
                <DetailRow label="Model family">
                  <Text size="supporting" mono>
                    {record.model_family}
                  </Text>
                </DetailRow>
              )}
            </Stack>

            {/* Routing — omitted when neither field has a value */}
            {(record.routing_strategy || record.retry_count > 0) && (
              <>
                <Separator />
                <Stack padding="snug">
                  <Stack paddingTop="none" paddingInline="snug" paddingBottom="tight">
                    <Text size="caption" color="muted">
                      Routing
                    </Text>
                  </Stack>
                  {record.routing_strategy && (
                    <DetailRow label="Routing strategy">
                      <Text size="supporting" mono>
                        {record.routing_strategy}
                      </Text>
                    </DetailRow>
                  )}
                  {record.retry_count > 0 && (
                    <DetailRow label="Retries">
                      <Text size="supporting" mono>
                        {record.retry_count}
                      </Text>
                    </DetailRow>
                  )}
                </Stack>
              </>
            )}

            <Separator />

            {/* Performance */}
            <Stack padding="snug">
              <Stack paddingTop="none" paddingInline="snug" paddingBottom="tight">
                <Text size="caption" color="muted">
                  Performance
                </Text>
              </Stack>
              <DetailRow label="Latency">
                <Text size="supporting" mono>
                  {record.latency_ms}ms
                </Text>
              </DetailRow>
              <DetailRow label="Input tokens">
                <Text size="supporting" mono>
                  {fmtNum(record.prompt_tokens)}
                </Text>
              </DetailRow>
              <DetailRow label="Output tokens">
                <Text size="supporting" mono>
                  {fmtNum(record.completion_tokens)}
                </Text>
              </DetailRow>
              {record.cache_read_input_tokens > 0 && (
                <DetailRow label="Cache read tokens">
                  <Text size="supporting" mono>
                    {fmtNum(record.cache_read_input_tokens)}
                  </Text>
                </DetailRow>
              )}
              {record.cache_creation_input_tokens > 0 && (
                <DetailRow label="Cache write tokens">
                  <Text size="supporting" mono>
                    {fmtNum(record.cache_creation_input_tokens)}
                  </Text>
                </DetailRow>
              )}
              <DetailRow label="Cache hit rate">
                <Text size="supporting" mono>
                  {record.cache_read_input_tokens > 0
                    ? `${((record.cache_read_input_tokens / (record.prompt_tokens + record.cache_read_input_tokens)) * 100).toFixed(1)}%`
                    : '—'}
                </Text>
              </DetailRow>
              <DetailRow label="Total tokens">
                <Text size="supporting" mono weight="emphasis">
                  {fmtNum(record.total_tokens)}
                </Text>
              </DetailRow>
              <DetailRow label="Estimated cost">
                <Text size="supporting" mono>
                  {fmtCost(record.estimated_cost_usd)}
                </Text>
              </DetailRow>
            </Stack>

            {/* Failure — shown for any non-success status */}
            {record.status !== 'success' && (
              <>
                <Separator />
                <Stack padding="snug">
                  <Stack paddingTop="none" paddingInline="snug" paddingBottom="tight">
                    <Text size="caption" color="muted">
                      Failure
                    </Text>
                  </Stack>
                  <DetailRow label="Status">
                    <Badge variant={getInferenceStatusVariant(record.status)} size="sm">
                      {fmtInferenceStatus(record.status)}
                    </Badge>
                  </DetailRow>
                  {record.error_code && (
                    <DetailRow label="Error code">
                      <Badge variant="error" size="sm">
                        {record.error_code}
                      </Badge>
                    </DetailRow>
                  )}
                  {record.failure_details && (
                    <>
                      {record.failure_details.provider_http_status !== undefined && (
                        <DetailRow label="HTTP status">
                          <Text size="supporting" mono>
                            {record.failure_details.provider_http_status}
                          </Text>
                        </DetailRow>
                      )}
                      {record.failure_details.provider_error_type && (
                        <DetailRow label="Error type">
                          <Badge variant="error" size="sm">
                            {record.failure_details.provider_error_type}
                          </Badge>
                        </DetailRow>
                      )}
                      {record.failure_details.provider_error_message && (
                        <Stack paddingBlock="tight" paddingInline="snug" gap="tight">
                          <Text size="caption" color="muted">
                            Provider message
                          </Text>
                          <Text size="supporting" color="foreground" mono>
                            {record.failure_details.provider_error_message}
                          </Text>
                        </Stack>
                      )}
                    </>
                  )}
                </Stack>
              </>
            )}
          </ModalBody>
        </>
      )}
    </Modal>
  );
}
