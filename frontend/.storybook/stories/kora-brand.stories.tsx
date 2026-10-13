import type { CSSProperties } from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { BrandLogo, Stack, Text } from '@runkora/hades';
import { defaultTheme } from '@orbit/theme/default';

const meta: Meta = {
  title: 'Kora/Brand',
  parameters: { layout: 'fullscreen' },
};

export default meta;

const STEEL = '#4d6b80';

// The mark is one circle cut in half. The lower half is moved right and drawn
// slightly larger. All sizes scale from a 150px circle.
interface MarkProps {
  size: number;
  upper?: string;
  lower?: string;
}

function Mark({ size, upper = 'var(--hades-raw-white)', lower = STEEL }: MarkProps) {
  const s = size / 150;
  const abs = (style: CSSProperties): CSSProperties => ({ position: 'absolute', ...style });
  return (
    <Stack style={{ position: 'relative', width: 172 * s, height: 151 * s, flex: 'none' }}>
      <Stack style={abs({ left: 0, top: 0, width: 150 * s, height: 72 * s, overflow: 'hidden' })}>
        <Stack
          style={{
            width: 150 * s,
            height: 150 * s,
            borderRadius: '50%',
            background: upper,
            flexShrink: 0,
          }}
        />
      </Stack>
      <Stack
        style={abs({
          left: 19 * s,
          top: 78 * s,
          width: 152 * s,
          height: 73 * s,
          overflow: 'hidden',
        })}
      >
        <Stack
          style={{
            width: 152 * s,
            height: 152 * s,
            borderRadius: '50%',
            background: lower,
            flexShrink: 0,
            marginTop: -79 * s,
          }}
        />
      </Stack>
    </Stack>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <Stack gap="comfortable">
      <Text size="supporting" color="muted">
        {title}
      </Text>
      {children}
    </Stack>
  );
}

export const Identity: StoryObj = {
  render: () => (
    <Stack gap="relaxed" style={{ padding: 48, maxWidth: 1100 }}>
      <Section title="Mark and wordmark">
        <BrandLogo
          dark={defaultTheme.logo.dark}
          light={defaultTheme.logo.light}
          size={150}
          title={defaultTheme.productName}
        />
      </Section>

      <Section title="Sizes and single-colour versions">
        <Stack direction="horizontal" gap="relaxed" align="center">
          <Mark size={48} />
          <Mark size={32} />
          <Mark size={16} />
          <Mark size={48} upper="var(--hades-raw-white)" lower="var(--hades-raw-white)" />
          <Mark size={16} upper="var(--hades-raw-white)" lower="var(--hades-raw-white)" />
        </Stack>
      </Section>
    </Stack>
  ),
};
