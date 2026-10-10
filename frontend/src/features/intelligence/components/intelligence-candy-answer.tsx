'use client';

import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { IconChevronDown, IconChevronRight } from '@tabler/icons-react';
import { Response } from '@/components/ai-elements/response';
import { normalizeCandyAnswerMarkdown } from './intelligence-candy-markdown';

interface Props {
  // answer is the text the model produced for the candy question. Empty when the
  // run failed before it answered.
  answer?: string | null;
}

// CandyAnswer renders the candy question's answer as prose.
//
// The answer is markdown the model wrote, with its arithmetic wrapped in LaTeX.
// The delimiters come off before rendering (see normalizeCandyAnswerMarkdown):
// the renderer's math step would otherwise paint each formula three times over,
// because the app carries neither KaTeX's stylesheet nor the class names its
// sanitize step strips. What is left is the markdown the renderer does handle
// well — bold, lists, and the tables some answers end with. The long reasoning
// stays collapsed until the reader asks for it.
const COLLAPSED_HEIGHT = 120;

export function CandyAnswer({ answer }: Props) {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState(false);
  const text = normalizeCandyAnswerMarkdown((answer ?? '').trim());

  if (text === '') {
    return (
      <div className='mt-3 rounded-md border border-dashed p-3'>
        <p className='text-muted-foreground text-xs'>{t('intelligence.candy.empty')}</p>
      </div>
    );
  }

  return (
    <div className='mt-3 rounded-md border'>
      <div className='flex items-center justify-between border-b px-3 py-2'>
        <span className='text-xs font-medium'>{t('intelligence.candy.answer')}</span>
        <button
          type='button'
          className='text-muted-foreground hover:text-foreground flex items-center gap-1 text-xs'
          onClick={() => setExpanded((value) => !value)}
          aria-expanded={expanded}
        >
          {expanded ? <IconChevronDown size={12} /> : <IconChevronRight size={12} />}
          {expanded ? t('intelligence.candy.collapse') : t('intelligence.candy.expand')}
        </button>
      </div>
      <div
        className='overflow-auto px-3 py-2'
        // The answer keeps its own wrapping: it is prose the model wrote, and
        // reflowing it into a fixed frame is what made it unreadable before.
        style={{ maxHeight: expanded ? 480 : COLLAPSED_HEIGHT }}
      >
        <Response className='text-xs leading-relaxed'>{text}</Response>
      </div>
    </div>
  );
}
