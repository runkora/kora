import { EmptyState } from '@orbit/components';
import { fetchSkillDetail } from '../_services/marketplace-server';
import { SkillDetail } from '../_components/marketplace-skill-detail';

interface SkillDetailPageProps {
  params: Promise<{ slug: string[] }>;
}

export default async function SkillDetailPage({ params }: SkillDetailPageProps) {
  const { slug: segments } = await params;
  const skill = await fetchSkillDetail(segments.join('/'));

  if (skill === null) {
    return (
      <EmptyState
        icon="box"
        title="Skill not found"
        description="No Skill in this Marketplace matches this address."
      />
    );
  }

  return <SkillDetail skill={skill} />;
}
