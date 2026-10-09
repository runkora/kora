export interface Marketplace {
  name: string;
}

export interface SkillSummary {
  name: string;
  description: string;
}

export interface SkillFile {
  path: string;
  name: string;
  content: string;
}

export interface Skill {
  name: string;
  description: string;
  skill: SkillFile;
  documents: SkillFile[];
}

export function marketplaceSlug(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-|-$/g, '');
}