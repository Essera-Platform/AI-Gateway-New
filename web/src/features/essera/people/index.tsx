import { useQuery } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import type { ColumnDef } from '@tanstack/react-table'
import { Plus } from 'lucide-react'
import { useCallback, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DataTablePage, useDataTable } from '@/components/data-table'
import { SectionPageLayout } from '@/components/layout'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { useMediaQuery } from '@/hooks'
import { useTableUrlState } from '@/hooks/use-table-url-state'

import { AddPersonDialog } from './add-person-dialog'
import { getPeople, type EsseraPerson, type EsseraPersonAction } from './api'
import { peopleMessage } from './messages'
import { PersonRowActions } from './person-row-actions'

const route = getRouteApi('/_authenticated/users/')

/**
 * What the admin changed that the Console has not shown back yet: people added, and the
 * expected state of people suspended, reactivated or removed. A row stays marked pending until
 * the Console's list agrees.
 */
type PendingChanges = {
  added: EsseraPerson[]
  changed: Record<string, EsseraPerson>
  removed: string[]
}

const NO_CHANGES: PendingChanges = { added: [], changed: {}, removed: [] }

function withPendingChanges(
  fetched: EsseraPerson[],
  pending: PendingChanges
): EsseraPerson[] {
  const listed = new Set(fetched.map((person) => person.id))
  const rows = fetched
    .filter((person) => !pending.removed.includes(person.id))
    .map((person) => {
      const expected = pending.changed[person.id]
      if (!expected || expected.status === person.status) return person
      return { ...person, status: expected.status, pending: true }
    })
  const added = pending.added.filter((person) => !listed.has(person.id))
  return [...added, ...rows]
}

function usePeopleColumns(
  onChanged: (person: EsseraPerson, action: EsseraPersonAction) => void
): ColumnDef<EsseraPerson, unknown>[] {
  const { t } = useTranslation()
  return useMemo(
    () => [
      {
        accessorKey: 'email',
        header: t('Email'),
        cell: ({ row }) => (
          <div className='flex min-w-0 flex-col'>
            <span className='text-foreground truncate font-semibold'>
              {row.original.email}
            </span>
            {row.original.name && (
              <span className='text-muted-foreground truncate text-xs'>
                {row.original.name}
              </span>
            )}
          </div>
        ),
        enableSorting: false,
        size: 280,
        meta: { mobileTitle: true },
      },
      {
        accessorKey: 'role',
        header: t('Role'),
        cell: ({ row }) => (
          <span className='text-muted-foreground'>
            {row.original.role === 'admin' ? t('Admin') : t('User')}
          </span>
        ),
        enableSorting: false,
        size: 140,
      },
      {
        accessorKey: 'status',
        header: t('Status'),
        cell: ({ row }) => {
          const active = row.original.status === 'active'
          return (
            <div className='flex items-center gap-1.5'>
              <StatusBadge
                label={active ? t('Active') : t('Suspended')}
                variant={active ? 'success' : 'warning'}
                copyable={false}
                showDot
              />
              {row.original.pending && (
                <StatusBadge
                  label={t('Pending')}
                  variant='info'
                  copyable={false}
                />
              )}
            </div>
          )
        },
        enableSorting: false,
        size: 180,
        meta: { mobileBadge: true },
      },
      {
        accessorKey: 'key_count',
        header: t('API Keys'),
        cell: ({ row }) => (
          <span className='num font-mono'>{row.original.key_count}</span>
        ),
        enableSorting: false,
        size: 100,
      },
      {
        id: 'actions',
        header: () => t('Actions'),
        cell: ({ row }) => (
          <PersonRowActions person={row.original} onChanged={onChanged} />
        ),
        meta: { pinned: 'right' as const },
      },
    ],
    [t, onChanged]
  )
}

/**
 * The customer's people, read from the Console, for its admin: add users, and
 * disable, enable or delete them. The Console carries the change out; limits, prices and roles
 * stay the Console's.
 */
export function EsseraPeople() {
  const { t } = useTranslation()
  const [addOpen, setAddOpen] = useState(false)
  const [pending, setPending] = useState<PendingChanges>(NO_CHANGES)
  const isMobile = useMediaQuery('(max-width: 640px)')
  const { pagination, onPaginationChange, ensurePageInRange } =
    useTableUrlState({
      search: route.useSearch(),
      navigate: route.useNavigate(),
      pagination: { defaultPage: 1, defaultPageSize: isMobile ? 10 : 20 },
    })

  const { data, isLoading, isFetching, error } = useQuery({
    queryKey: [
      'essera',
      'people',
      pagination.pageIndex + 1,
      pagination.pageSize,
    ],
    queryFn: async () => {
      const result = await getPeople({
        p: pagination.pageIndex + 1,
        page_size: pagination.pageSize,
      })
      if (!result.success) {
        throw new Error(peopleMessage(t, result))
      }
      return { items: result.data?.items ?? [], total: result.data?.total ?? 0 }
    },
    placeholderData: (previous) => previous,
    meta: { errorToast: false },
  })

  const onChanged = useCallback(
    (person: EsseraPerson, action: EsseraPersonAction) => {
      setPending((current) =>
        action === 'remove'
          ? { ...current, removed: [...current.removed, person.id] }
          : { ...current, changed: { ...current.changed, [person.id]: person } }
      )
    },
    []
  )
  const columns = usePeopleColumns(onChanged)
  const rows = useMemo(
    () => withPendingChanges(data?.items ?? [], pending),
    [data, pending]
  )

  const { table } = useDataTable({
    data: rows,
    columns,
    pagination,
    onPaginationChange,
    manualPagination: true,
    totalCount: data?.total ?? 0,
    ensurePageInRange,
  })

  return (
    <>
      <SectionPageLayout fixedContent>
        <SectionPageLayout.Title>{t('Users')}</SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <Button className='h-9 px-4' onClick={() => setAddOpen(true)}>
            <Plus className='h-4 w-4' />
            {t('Add User')}
          </Button>
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <DataTablePage
            table={table}
            columns={columns}
            isLoading={isLoading}
            isFetching={isFetching}
            emptyTitle={error ? error.message : t('No Users Found')}
            emptyDescription={
              error ? '' : t('Add the users who may use this gateway.')
            }
            skeletonKeyPrefix='essera-people-skeleton'
            applyHeaderSize
            toolbarProps={null}
          />
        </SectionPageLayout.Content>
      </SectionPageLayout>
      <AddPersonDialog
        open={addOpen}
        onOpenChange={setAddOpen}
        onAdded={(person) =>
          setPending((current) => ({
            ...current,
            added: [person, ...current.added],
          }))
        }
      />
    </>
  )
}
