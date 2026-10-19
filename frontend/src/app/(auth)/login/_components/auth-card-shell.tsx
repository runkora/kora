'use client';

/**
 * @tier 3 — login-scoped. The sign-in card shared by the login and already-signed-in
 * screens: a brand panel on the left, a title, content and a footer on the right.
 * The panel is hidden on small screens, where the logo moves into the form column.
 * The logo and product name come from the effective brand.
 */
import type { ReactNode } from 'react';
import { BrandLogo, Card, CardFooter, Heading, Show, Stack, Text } from '@runkora/hades';
import { useBrand } from '@orbit/theme/brand-provider';

const PANEL_GRADIENT =
  'linear-gradient(25deg, color-mix(in oklch, var(--color-border-accent) 55%, var(--color-surface-elevated)) 0%, var(--color-surface-elevated) 55%, var(--color-surface) 100%)';

interface AuthCardShellProps {
  title: string;
  description?: string;
  /** The strip under the form side. */
  footer: ReactNode;
  children?: ReactNode;
}

export function AuthCardShell({ title, description, footer, children }: AuthCardShellProps) {
  const brand = useBrand();
  return (
    <Card style={{ width: '100%', maxWidth: 740, overflow: 'hidden' }}>
      <Stack direction="horizontal" gap="none" align="stretch">
        <Show above="md">
          <Stack
            align="center"
            justify="center"
            padding="loose"
            style={{
              width: 330,
              height: '100%',
              minHeight: 400,
              background: PANEL_GRADIENT,
              borderRight: '1px solid var(--color-border-subtle)',
            }}
          >
            <Stack align="center" gap="relaxed" textAlign="center">
              <Heading variant="title">
                <BrandLogo
                  dark={brand.logo.dark}
                  light={brand.logo.light}
                  size={40}
                  title={brand.productName}
                />
              </Heading>
              <Stack style={{ textWrap: 'balance' }}>
                <Heading variant="section">Governed passage for agents.</Heading>
              </Stack>
            </Stack>
          </Stack>
        </Show>

        <Stack grow style={{ minWidth: 0 }}>
          <Stack grow align="center" justify="center" padding="loose">
            <Stack gap="relaxed" style={{ width: '100%', maxWidth: 305 }}>
              <Show below="md">
                <Heading variant="title">
                  <BrandLogo
                    dark={brand.logo.dark}
                    light={brand.logo.light}
                    size={34}
                    title={brand.productName}
                  />
                </Heading>
              </Show>
              <Stack gap="snug">
                <Heading variant="section">{title}</Heading>
                {description && (
                  <Text size="supporting" color="muted">
                    {description}
                  </Text>
                )}
              </Stack>
              {children}
            </Stack>
          </Stack>

          {/* CardFooter already applies border-t border-border-subtle */}
          <CardFooter compact justify="center">
            {footer}
          </CardFooter>
        </Stack>
      </Stack>
    </Card>
  );
}
