'use client';

/**
 * @tier 3 — triggers-scoped. Loads what a webhook trigger can listen for and
 * read: the provider's schema event types, sender associations and the chosen
 * event's fields, plus the event types an existing source has actually sent.
 */

import { useEffect, useState } from 'react';
import type { TriggerField, TriggerProviderEventTypes } from '@orbit/shared/api';
import {
  fetchEventTypes,
  fetchProviderEventTypes,
  fetchProviderFields,
} from '../_services/triggers-api';

export interface SourceCatalog {
  declared: string[];
  observed: string[];
  /** Empty when the provider identifies no sender. */
  associations: string[];
  /** Fields of the chosen event type; empty until one is chosen. */
  fields: TriggerField[];
  error: string | null;
}

/** An answer tagged with the request it answers. */
interface Answer<T> {
  key: string;
  value: T;
  error: string | null;
}

/**
 * `provider` empty means nothing to ask yet. `sourceId` empty means a new
 * source with no received event types.
 */
export function useSourceCatalog(
  provider: string,
  sourceId: string,
  eventType: string,
): SourceCatalog {
  const [types, setTypes] = useState<Answer<TriggerProviderEventTypes | null> | null>(null);
  const [observed, setObserved] = useState<Answer<string[]> | null>(null);
  const [fields, setFields] = useState<Answer<TriggerField[]> | null>(null);
  const fieldsKey = provider && eventType ? `${provider}/${eventType}` : '';

  useEffect(() => {
    if (!provider) return;
    // A response for a provider no longer chosen must not overwrite the current one.
    let live = true;
    void fetchProviderEventTypes(provider).then(({ eventTypes, error }) => {
      if (live) setTypes({ key: provider, value: eventTypes, error });
    });
    return () => {
      live = false;
    };
  }, [provider]);

  useEffect(() => {
    if (!sourceId) return;
    let live = true;
    void fetchEventTypes(sourceId).then(({ eventTypes, error }) => {
      if (live) setObserved({ key: sourceId, value: eventTypes?.observed ?? [], error });
    });
    return () => {
      live = false;
    };
  }, [sourceId]);

  useEffect(() => {
    if (!fieldsKey) return;
    let live = true;
    void fetchProviderFields(provider, eventType).then((res) => {
      if (live) setFields({ key: fieldsKey, value: res.fields, error: res.error });
    });
    return () => {
      live = false;
    };
  }, [fieldsKey, provider, eventType]);

  const currentTypes = types !== null && types.key === provider ? types : null;
  const currentObserved = observed !== null && observed.key === sourceId ? observed : null;
  const currentFields = fields !== null && fields.key === fieldsKey ? fields : null;
  return {
    declared: currentTypes?.value?.declared ?? [],
    observed: currentObserved?.value ?? [],
    associations: currentTypes?.value?.associations ?? [],
    fields: currentFields?.value ?? [],
    error: currentTypes?.error ?? currentObserved?.error ?? currentFields?.error ?? null,
  };
}
