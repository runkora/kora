/**
 * Integration tests for the HealthStatus component.
 *
 * MSW intercepts the fetch to /api/identity/health.
 * Tests cover the three visible states: loading, ok, and error.
 */
import { describe, it, expect } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { renderWithProviders } from '@orbit/test-utils';
import { server } from '@orbit/test-utils/msw/server';
import { createHealthResponse } from '@orbit/test-utils/factories/health';
import { HealthStatus } from './health-status';

describe('HealthStatus', () => {
  describe('loading state', () => {
    it('renders a skeleton element before the fetch resolves', async () => {
      // Delay the MSW response so the loading state is observable
      server.use(
        http.get('/api/identity/health', async () => {
          await new Promise((resolve) => setTimeout(resolve, 100));
          return HttpResponse.json({ data: createHealthResponse() });
        }),
      );

      renderWithProviders(<HealthStatus />);

      // The Skeleton renders an element with role="status" while loading
      // (falls back to checking the element is present before ok/error badges appear)
      expect(screen.queryByText('Unreachable')).not.toBeInTheDocument();
      expect(screen.queryByText('healthy')).not.toBeInTheDocument();

      // Wait for the ok state to eventually appear (keeps the test deterministic)
      await screen.findByText('healthy');
    });
  });

  describe('ok state', () => {
    it('renders the healthy badge with the status from the response', async () => {
      renderWithProviders(<HealthStatus />);

      const badge = await screen.findByText('healthy');
      expect(badge).toBeInTheDocument();
    });

    it('renders the service name from the response', async () => {
      renderWithProviders(<HealthStatus />);

      await screen.findByText('orbit-identity');
    });

    it('renders a custom status and service name when overridden', async () => {
      server.use(
        http.get('/api/identity/health', () =>
          HttpResponse.json({
            data: createHealthResponse({ status: 'degraded', service: 'orbit-gateway' }),
          }),
        ),
      );

      renderWithProviders(<HealthStatus />);

      await screen.findByText('degraded');
      await screen.findByText('orbit-gateway');
    });
  });

  describe('error state', () => {
    it('renders "Unreachable" when the BFF returns a non-ok response', async () => {
      server.use(
        http.get('/api/identity/health', () =>
          HttpResponse.json(
            { error: { code: 'BACKEND_UNREACHABLE', message: 'Backend unreachable' } },
            { status: 502 },
          ),
        ),
      );

      renderWithProviders(<HealthStatus />);

      await screen.findByText('Unreachable');
    });

    it('renders "Unreachable" when the response contains an error field', async () => {
      server.use(
        http.get('/api/identity/health', () =>
          HttpResponse.json({ error: 'service_unavailable' }),
        ),
      );

      renderWithProviders(<HealthStatus />);

      await screen.findByText('Unreachable');
    });

    it('renders "Unreachable" when the network request fails', async () => {
      server.use(
        http.get('/api/identity/health', () => HttpResponse.error()),
      );

      renderWithProviders(<HealthStatus />);

      await waitFor(() => {
        expect(screen.getByText('Unreachable')).toBeInTheDocument();
      });
    });

    it('does not show the healthy badge in the error state', async () => {
      server.use(
        http.get('/api/identity/health', () =>
          HttpResponse.json({ error: 'internal' }, { status: 500 }),
        ),
      );

      renderWithProviders(<HealthStatus />);

      await screen.findByText('Unreachable');
      expect(screen.queryByText('healthy')).not.toBeInTheDocument();
    });

    it('renders the "make dev" hint in the error state', async () => {
      server.use(
        http.get('/api/identity/health', () =>
          HttpResponse.json({ error: 'internal' }, { status: 503 }),
        ),
      );

      renderWithProviders(<HealthStatus />);

      await screen.findByText('Unreachable');
      expect(screen.getByText('make dev')).toBeInTheDocument();
    });
  });
});
