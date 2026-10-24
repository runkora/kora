/** @tier 2 — kora-domain-aware: marks items declared in a tenant's configuration repository. Used by: agents, environments, triggers, policy roles, model gateway catalog. */
import { Badge, Callout, Text } from '@runkora/hades';

/** Marks an item declared in the configuration repository. Show it wherever the item is listed or opened. */
export function ManagedInGitBadge() {
  return (
    <Badge variant="secondary" size="sm">
      Managed in git
    </Badge>
  );
}

export interface ManagedInGitCalloutProps {
  /** What is read-only, as the plural subject that starts the sentence, e.g. "Agents in this tenant". */
  subject: string;
}

/** Explains why a page is read-only when its items are authored only in the configuration repository. */
export function ManagedInGitCallout({ subject }: ManagedInGitCalloutProps) {
  return (
    <Callout>
      <Text size="supporting">
        {subject} are managed in git. Make changes in the configuration repository.
      </Text>
    </Callout>
  );
}
