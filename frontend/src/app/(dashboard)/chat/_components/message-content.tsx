/**
 * @tier 3 — chat scoped. Renders AI/user message content: plain text with
 * whitespace preserved, fenced code blocks rendered as styled figures with
 * copy button.
 * @promote-to-tier-2 if: AI-generated text rendering is needed outside the
 * chat page (e.g. policy explain, agent scratchpad viewer).
 */
import { Button, Code, Icon, Stack, Text } from '@orbit/terra';

export interface MessageContentProps {
  content: string;
}

export function MessageContent({ content }: MessageContentProps) {
  const segments = content.split(/(```(?:[a-z]*\n)?[\s\S]*?```)/g);

  return (
    <Stack gap="snug" asChild>
      <article className="text-sm leading-relaxed text-foreground">
        {segments.map((seg, i) => {
          const codeMatch = seg.match(/^```([a-z]*)\n?([\s\S]*?)```$/);
          if (codeMatch) {
            const lang = codeMatch[1] || 'code';
            const code = codeMatch[2].trim();
            return (
              <Stack key={i} gap="none" asChild>
                <figure className="rounded-lg border border-border-subtle overflow-hidden m-0">
                  <Stack direction="horizontal" align="center" justify="between" asChild>
                    <figcaption className="px-4 py-2 bg-surface border-b border-border-subtle">
                      <Text size="micro" color="subtle" mono>
                        {lang}
                      </Text>
                      <Button variant="ghost" size="icon-sm" aria-label="Copy code">
                        <Icon name="copy" size="xs" />
                      </Button>
                    </figcaption>
                  </Stack>
                  <pre className="bg-background px-4 py-3 overflow-x-auto">
                    <Code variant="bare">{code}</Code>
                  </pre>
                </figure>
              </Stack>
            );
          }
          return seg ? (
            <Text key={i} as="span" preserveWhitespace>
              {seg}
            </Text>
          ) : null;
        })}
      </article>
    </Stack>
  );
}
