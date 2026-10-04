'use client';

import { useTranslation } from 'react-i18next';
import { Badge } from '@/components/ui/badge';
import { IntelligenceVerdict, verdictBadgeVariant } from './intelligence-verdict';

interface Props {
  // html is the source the tested model produced, exactly as scored.
  html?: string | null;
  verdict: IntelligenceVerdict;
}

// IntelligenceHTMLPreview renders the generated document in a sandbox and
// overlays the verdict on its top-right corner.
//
// The source is untrusted twice over: a third-party model produced it, and it
// arrived through a third-party relay. The iframe therefore runs with
// `allow-scripts` only. Adding `allow-same-origin` here would let the document
// reach back into this app's origin and storage, which defeats the sandbox
// entirely, so it is deliberately absent.
export function IntelligenceHTMLPreview({ html, verdict }: Props) {
  const { t } = useTranslation();
  const badge = (
    <Badge variant={verdictBadgeVariant(verdict)} className='absolute right-2 top-2 z-10 shadow-sm'>
      {t(`channels.dialogs.intelligence.verdict.${verdict}`)}
    </Badge>
  );

  if (!html) {
    return (
      <div className='relative mt-3 rounded-md border border-dashed'>
        {badge}
        <p className='px-3 py-6 text-center text-xs text-muted-foreground'>{t('channels.dialogs.intelligence.preview.missing')}</p>
      </div>
    );
  }

  return (
    <div className='relative mt-3 overflow-hidden rounded-md border bg-white'>
      {badge}
      <iframe
        sandbox='allow-scripts'
        srcDoc={html}
        title={t('channels.dialogs.intelligence.preview.title')}
        className='h-64 w-full'
      />
    </div>
  );
}
