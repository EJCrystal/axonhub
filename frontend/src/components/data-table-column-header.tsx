import { ArrowDownIcon, ArrowUpIcon, CaretSortIcon, EyeNoneIcon } from '@radix-ui/react-icons';
import { Column } from '@tanstack/react-table';
import { useTranslation } from 'react-i18next';
import { cn } from '@/lib/utils';
import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';

interface DataTableColumnHeaderProps<TData, TValue> extends React.HTMLAttributes<HTMLDivElement> {
  column: Column<TData, TValue>;
  title: string;
}

export function DataTableColumnHeader<TData, TValue>({ column, title, className }: DataTableColumnHeaderProps<TData, TValue>) {
  const { t } = useTranslation();

  if (!column.getCanSort()) {
    return <div className={cn('text-muted-foreground min-w-0 truncate text-xs leading-5 font-medium', className)}>{title}</div>;
  }

  return (
    <div className={cn('flex min-w-0 items-center space-x-2', className)}>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant='ghost'
            size='sm'
            className='data-[state=open]:bg-accent text-muted-foreground -ml-3 h-8 min-w-0 shrink text-xs leading-5 font-medium'
          >
            <span className='min-w-0 truncate'>{title}</span>
            <span className='flex min-w-0 shrink-[9999] overflow-hidden'>
              {column.getIsSorted() === 'desc' ? (
                <ArrowDownIcon className='ml-2 h-4 w-4' />
              ) : column.getIsSorted() === 'asc' ? (
                <ArrowUpIcon className='ml-2 h-4 w-4' />
              ) : (
                <CaretSortIcon className='ml-2 h-4 w-4' />
              )}
            </span>
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align='start'>
          <DropdownMenuItem onClick={() => column.toggleSorting(false)}>
            <ArrowUpIcon className='text-muted-foreground/70 mr-2 h-3.5 w-3.5' />
            {t('common.sort.ascending')}
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => column.toggleSorting(true)}>
            <ArrowDownIcon className='text-muted-foreground/70 mr-2 h-3.5 w-3.5' />
            {t('common.sort.descending')}
          </DropdownMenuItem>
          {column.getCanHide() && (
            <>
              <DropdownMenuSeparator />
              <DropdownMenuItem onClick={() => column.toggleVisibility(false)}>
                <EyeNoneIcon className='text-muted-foreground/70 mr-2 h-3.5 w-3.5' />
                {t('common.sort.hide')}
              </DropdownMenuItem>
            </>
          )}
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}
