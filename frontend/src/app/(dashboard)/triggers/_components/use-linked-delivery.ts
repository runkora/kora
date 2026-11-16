'use client';

/**
 * @tier 3: triggers-scoped. The delivery a `?delivery=` link names, fetched by id so the
 * page's range cannot hide an older one.
 */

import { useEffect, useState } from 'react';
import { getDelivery } from '../_services/triggers-api';
import { toDeliveryRow } from '../_types';
import type { DeliveryRow } from '../_types';

interface LinkedDelivery {
  delivery: DeliveryRow | null;
  error: string | null;
}

/** `id` null fetches nothing. */
export function useLinkedDelivery(id: string | null): LinkedDelivery {
  const [state, setState] = useState<LinkedDelivery & { id: string | null }>({
    id: null,
    delivery: null,
    error: null,
  });
  useEffect(() => {
    if (id === null) return;
    let live = true;
    void getDelivery(id).then(({ delivery, error }) => {
      if (live) setState({ id, delivery: delivery && toDeliveryRow(delivery), error });
    });
    return () => {
      live = false;
    };
  }, [id]);
  return state.id === id ? state : { delivery: null, error: null };
}
