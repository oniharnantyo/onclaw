import Skills from '../components/Skills';
import type { Skill } from '../components/Skills';

interface SkillsPageProps {
  skills: Skill[];
  loadSkills: () => Promise<void>;
  showToast: (message: string, type?: 'success' | 'error') => void;
}

export default function SkillsPage({ skills, loadSkills, showToast }: SkillsPageProps) {
  return <Skills skills={skills} loadSkills={loadSkills} showToast={showToast} />;
}
