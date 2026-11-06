/** @tier 3 — trigger-detail-modal-scoped. Promote to src/components/ if a second feature needs this. */
import { Anchor, Callout, Stack, Text } from '@runkora/hades';
import { repositoryUrl } from '@runkora/lib/declarative';

/** Who manages the trigger: its repository when declared, else the tenant's lock. */
export function managedSubject(declared: boolean, repository: string | undefined): string {
  return declared && repository ? `Managed by ${repository}` : 'Managed in git';
}

/**
 * The one-line explanation shown beside disabled save actions: where to make
 * the change, and that nothing typed here is kept.
 */
export function managedFooterMessage(declared: boolean, repository: string | undefined): string {
  const where = declared && repository ? repository : 'the configuration repository';
  return `${managedSubject(declared, repository)}. Edit it in ${where}; changes made here are not saved.`;
}

/**
 * Heads every Configuration section of a trigger managed in git, so the reason
 * its controls are dead is in view wherever the operator is looking.
 */
export function ManagedSectionNotice({
  declared,
  repository,
  extra,
}: {
  declared: boolean;
  repository?: string;
  /** What this section still allows or does not offer, in a sentence. */
  extra?: string;
}) {
  const url = declared && repository ? repositoryUrl(repository) : null;
  return (
    <Callout variant="info" aria-label="Managed in git">
      <Stack gap="tight">
        <Text size="supporting" weight="heavy">
          {declared && repository ? (
            <>
              Managed by{' '}
              {url ? (
                <Anchor href={url} target="_blank" rel="noopener noreferrer">
                  {repository}
                </Anchor>
              ) : (
                repository
              )}
            </>
          ) : (
            'Managed in git'
          )}
        </Text>
        <Text size="caption" color="muted">
          Edit this trigger in{' '}
          {declared && repository ? 'that repository' : 'the configuration repository'}. Changes
          made here are not saved.
          {extra !== undefined && ` ${extra}`}
        </Text>
      </Stack>
    </Callout>
  );
}
