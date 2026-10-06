'use client';

import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { IconArrowsMaximize } from '@tabler/icons-react';
import { Badge } from '@/components/ui/badge';
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from '@/components/ui/dialog';
import { IntelligenceVerdict, verdictBadgeVariant } from './intelligence-verdict';

interface Props {
  // html is the source the tested model produced, exactly as scored.
  html?: string | null;
  verdict: IntelligenceVerdict;
}

// The generated documents are full-viewport scenes (typically one SVG sized
// with 100vw/100vh). Rendering them in an iframe the size of the card makes
// 100vw/100vh resolve to a few hundred pixels, and anything the document sizes
// in absolute units, such as a min-height, then overflows and gets clipped.
//
// So the preview renders the document in a fixed 1280x720 stage and scales the
// whole stage down to whatever space the card has. The document keeps its
// intended layout and nothing is cut off, no matter how it was authored.
const STAGE_WIDTH = 1280;
const STAGE_HEIGHT = 720;

// Stage renders one document at a fixed size and scales it to fit the box.
function Stage({ html, title, className }: { html: string; title: string; className: string }) {
  const boxRef = useRef<HTMLDivElement>(null);
  const [scale, setScale] = useState(1);

  useEffect(() => {
    const box = boxRef.current;
    if (!box) return;

    const fit = () => {
      const { width, height } = box.getBoundingClientRect();
      if (width === 0 || height === 0) return;
      setScale(Math.min(width / STAGE_WIDTH, height / STAGE_HEIGHT));
    };

    fit();
    const observer = new ResizeObserver(fit);
    observer.observe(box);
    return () => observer.disconnect();
  }, []);

  return (
    <div ref={boxRef} className={className}>
      <iframe
        sandbox='allow-scripts'
        srcDoc={html}
        title={title}
        width={STAGE_WIDTH}
        height={STAGE_HEIGHT}
        style={{ transform: `scale(${scale})`, transformOrigin: 'top left' }}
        className='block border-0'
      />
    </div>
  );
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

  const title = t('channels.dialogs.intelligence.preview.title');

  return (
    <div className='relative mt-3 overflow-hidden rounded-md border bg-white'>
      {badge}
      <div className='absolute bottom-2 right-2 z-10'>
        <Dialog>
          <DialogTrigger asChild>
            <button
              type='button'
              className='bg-background/90 hover:bg-background rounded-md border p-1.5 shadow-sm'
              aria-label={t('channels.dialogs.intelligence.preview.expand')}
              title={t('channels.dialogs.intelligence.preview.expand')}
            >
              <IconArrowsMaximize size={14} />
            </button>
          </DialogTrigger>
          <DialogContent className='flex h-[92vh] w-[95vw] max-w-[95vw] flex-col sm:max-w-[95vw]'>
            <DialogHeader>
              <DialogTitle className='flex items-center gap-2'>
                {title}
                <Badge variant={verdictBadgeVariant(verdict)}>
                  {t(`channels.dialogs.intelligence.verdict.${verdict}`)}
                </Badge>
              </DialogTitle>
            </DialogHeader>
            <div className='min-h-0 flex-1 overflow-hidden rounded-md border bg-white'>
              <Stage html={html} title={title} className='h-full w-full overflow-hidden' />
            </div>
          </DialogContent>
        </Dialog>
      </div>
      <Stage html={html} title={title} className='aspect-video w-full overflow-hidden' />
    </div>
  );
}
