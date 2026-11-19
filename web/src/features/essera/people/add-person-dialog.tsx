import { zodResolver } from '@hookform/resolvers/zod'
import { Loader2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { handleServerError } from '@/lib/handle-server-error'

import {
  getPersonSchema,
  toNewPerson,
  type PersonValues,
} from '../lib/person-schema'
import { addPerson, type EsseraPerson } from './api'
import { peopleMessage } from './messages'

type AddPersonDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onAdded: (person: EsseraPerson) => void
}

const EMPTY: PersonValues = { email: '', name: '', externalId: '' }

/**
 * Adds a user to the admin's customer at the Console, which creates their sign-in. They can sign in
 * once the Console publishes them to the Gateway.
 */
export function AddPersonDialog(props: AddPersonDialogProps) {
  const { t } = useTranslation()
  const [isSubmitting, setIsSubmitting] = useState(false)
  const schema = useMemo(() => getPersonSchema(t), [t])
  const form = useForm<PersonValues>({
    resolver: zodResolver(schema),
    defaultValues: EMPTY,
  })

  const close = (open: boolean) => {
    if (!open) form.reset(EMPTY)
    props.onOpenChange(open)
  }

  const onSubmit = async (values: PersonValues) => {
    setIsSubmitting(true)
    try {
      const result = await addPerson(toNewPerson(values))
      if (!result.success || !result.data) {
        toast.error(peopleMessage(t, result))
        return
      }
      toast.success(
        t('User added. They can sign in once the Console publishes them.')
      )
      props.onAdded({ ...result.data, pending: true })
      close(false)
    } catch (error) {
      handleServerError(error, t('Data is temporarily unavailable'))
    } finally {
      setIsSubmitting(false)
    }
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={close}
      title={t('Add User')}
      description={t(
        'The user signs in with their email. Their limits and prices are set in the Essera Operations Console.'
      )}
      contentClassName='sm:max-w-md'
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            onClick={() => close(false)}
            disabled={isSubmitting}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='submit'
            form='essera-add-person'
            disabled={isSubmitting}
          >
            {isSubmitting && <Loader2 className='animate-spin' />}
            {t('Create')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <form
          id='essera-add-person'
          onSubmit={form.handleSubmit(onSubmit)}
          className='grid gap-4'
        >
          <FormField
            control={form.control}
            name='email'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Email')}</FormLabel>
                <FormControl>
                  <Input type='email' autoComplete='off' {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='name'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Name')}</FormLabel>
                <FormControl>
                  <Input autoComplete='off' {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='externalId'
            render={({ field }) => (
              <FormItem>
                <FormLabel>
                  {t('External ID')}{' '}
                  <span className='text-muted-foreground font-normal'>
                    ({t('Optional')})
                  </span>
                </FormLabel>
                <FormControl>
                  <Input autoComplete='off' {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        </form>
      </Form>
    </Dialog>
  )
}
