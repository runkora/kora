import { cn } from '@orbit/terra';
import type { Provider } from '../../_data/mock';

// Each provider has a distinctive coloured letter-mark as a fallback icon.
// Replace `src` with a real asset path when provider logos are available.

const PROVIDER_CONFIG: Record<Provider, { label: string; bg: string; text: string }> = {
  Anthropic: { label: 'A', bg: 'bg-[#d4a27e]/20', text: 'text-[#c9783a]' },
  OpenAI: { label: 'O', bg: 'bg-[#74aa9c]/20', text: 'text-[#10a37f]' },
  Google: { label: 'G', bg: 'bg-[#4285f4]/20', text: 'text-[#4285f4]' },
  Mistral: { label: 'M', bg: 'bg-[#fa6c2e]/20', text: 'text-[#fa6c2e]' },
  Meta: { label: 'Λ', bg: 'bg-[#0081fb]/20', text: 'text-[#0081fb]' },
};

interface ProviderIconProps {
  provider: Provider;
  size?: 'sm' | 'md';
  className?: string;
}

export function ProviderIcon({ provider, size = 'sm', className }: ProviderIconProps) {
  const config = PROVIDER_CONFIG[provider];
  const sizeClasses = size === 'sm' ? 'size-5 text-[10px]' : 'size-7 text-xs';

  return (
    <span
      aria-hidden
      className={cn(
        'inline-flex items-center justify-center rounded font-bold shrink-0 select-none',
        config.bg,
        config.text,
        sizeClasses,
        className,
      )}
    >
      {config.label}
    </span>
  );
}
