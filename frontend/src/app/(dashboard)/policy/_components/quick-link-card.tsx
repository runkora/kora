/**
 * @tier 3 — policy scoped. Navigation card with icon, title, and description.
 * @promote-to-tier-2 if: another feature overview section needs nav/shortcut cards.
 */
import { Card, CardContent, CardHeader, CardTitle, Text } from '@runkora/hades';
import Link from 'next/link';

export interface QuickLinkCardProps {
  icon: React.ReactNode;
  title: string;
  description: string;
  href: string;
}

export function QuickLinkCard({ icon, title, description, href }: QuickLinkCardProps) {
  return (
    <Link href={href} className="block transition-colors [&>*]:hover:border-accent/40">
      <Card column>
        <CardHeader compact icon={icon}>
          <CardTitle>{title}</CardTitle>
        </CardHeader>
        <CardContent>
          <Text size="caption" color="muted">
            {description}
          </Text>
        </CardContent>
      </Card>
    </Link>
  );
}
