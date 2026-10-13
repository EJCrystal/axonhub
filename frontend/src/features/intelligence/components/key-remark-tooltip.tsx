'use client';

import { ReactNode } from 'react';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';

interface Props {
  // What to show while the pointer rests on the key. Empty means the key has no
  // remark, and the wrapper renders the children with no tooltip at all.
  content?: ReactNode;
  children: ReactNode;
  className?: string;
}

// KeyRemarkTooltip reveals a key's remark while the pointer rests on the key.
//
// The wrapper is a span so it can hold the selects and the table text that
// display keys without breaking their layout, and it is always rendered so a key
// with and without a remark occupies the same box.
export function KeyRemarkTooltip({ content, children, className }: Props) {
  const trigger = <span className={className}>{children}</span>;

  if (content === undefined || content === null || content === '' || (Array.isArray(content) && content.length === 0)) {
    return trigger;
  }

  return (
    <Tooltip>
      <TooltipTrigger asChild>{trigger}</TooltipTrigger>
      <TooltipContent className='max-w-72'>{content}</TooltipContent>
    </Tooltip>
  );
}
