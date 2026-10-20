'use client';

/**
 * @tier 3 · triggers-scoped. Loads what a webhook or chat command trigger can
 * listen for and read: the schema event types, the source's category, whether
 * senders are identified and the roles they can have, the chosen event's
 * fields, plus the event types an existing source has actually sent. Also
 * reads each provider's category, so a kind lists only its own providers.
 */

import { useEffect, useState } from 'react';
import type {
  SenderRole,
  SourceCategory,
  TriggerEventTypes,
  TriggerField,
} from '@runkora/shared/api';
import {
  fetchEventTypes,
  fetchProviderEventTypes,
  fetchProviderFields,
} from '../_services/triggers-api';

export interface SourceCatalog {
  declared: string[];
  observed: string[];
  /** The roles a sender restriction may name; empty when the provider reports none. */
  roles: SenderRole[];
  /** Whether the provider says who caused an event, so a sender restriction applies. */
  identifiesSenders: boolean;
  /** Fields of the chosen event type; empty until one is chosen. */
  fields: TriggerField[];
  /** Whether `fields` is the answer for the chosen event type. */
  fieldsReady: boolean;
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
 * source: its event types come from the provider, with none received.
 */
export function useSourceCatalog(
  provider: string,
  sourceId: string,
  eventType: string,
): SourceCatalog {
  const [types, setTypes] = useState<Answer<TriggerEventTypes | null> | null>(null);
  const [fields, setFields] = useState<Answer<TriggerField[]> | null>(null);
  const typesKey = sourceId ? `source/${sourceId}` : provider ? `provider/${provider}` : '';
  const fieldsKey = provider && eventType ? `${provider}/${eventType}` : '';

  useEffect(() => {
    if (!typesKey) return;
    // A response for a source or provider no longer chosen must not overwrite the current one.
    let live = true;
    const load = sourceId ? fetchEventTypes(sourceId) : fetchProviderEventTypes(provider);
    void load.then(({ eventTypes, error }) => {
      if (live) setTypes({ key: typesKey, value: eventTypes, error });
    });
    return () => {
      live = false;
    };
  }, [typesKey, sourceId, provider]);

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

  const currentTypes = types !== null && types.key === typesKey ? types : null;
  const currentFields = fields !== null && fields.key === fieldsKey ? fields : null;
  return {
    declared: currentTypes?.value?.declared ?? [],
    observed: currentTypes?.value?.observed ?? [],
    roles: currentTypes?.value?.senders?.roles ?? [],
    identifiesSenders: currentTypes?.value?.senders !== undefined,
    fields: currentFields?.value ?? [],
    fieldsReady: currentFields !== null && currentFields.error === null,
    error: currentTypes?.error ?? currentFields?.error ?? null,
  };
}

/** Each provider's category, and the providers whose answer has not arrived or failed. */
export interface ProviderCategories {
  categories: Record<string, SourceCategory>;
  pending: string[];
  failed: string[];
}

/**
 * Each provider's category, read from its event types. A provider is absent
 * from `categories` until its answer arrives, and is listed in `failed` when
 * the request fails.
 */
export function useProviderCategories(providers: readonly string[]): ProviderCategories {
  const [answers, setAnswers] = useState<Record<string, SourceCategory | null>>({});
  const key = providers.join(',');

  useEffect(() => {
    let live = true;
    for (const provider of key.split(',').filter(Boolean)) {
      void fetchProviderEventTypes(provider).then(({ eventTypes }) => {
        if (!live) return;
        setAnswers((prev) => ({ ...prev, [provider]: eventTypes?.category ?? null }));
      });
    }
    return () => {
      live = false;
    };
  }, [key]);

  const categories: Record<string, SourceCategory> = {};
  const pending: string[] = [];
  const failed: string[] = [];
  for (const provider of providers) {
    const answer = answers[provider];
    if (answer === undefined) pending.push(provider);
    else if (answer === null) failed.push(provider);
    else categories[provider] = answer;
  }
  return { categories, pending, failed };
}
