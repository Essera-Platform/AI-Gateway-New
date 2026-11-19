import { zodResolver } from '@hookform/resolvers/zod'
import { KeyRound } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { DateTimePicker } from '@/components/datetime-picker'
import {
  SideDrawerSection,
  SideDrawerSectionHeader,
  sideDrawerContentClassName,
  sideDrawerFooterClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { handleServerError } from '@/lib/handle-server-error'
import {
  getServerErrorMessage,
  translateServerText,
} from '@/lib/server-error-message'

import { createEsseraKey, type EsseraCreatedKey } from './api'
import { KeyCreatedDialog } from './key-created-dialog'
import {
  KEY_LIFETIME_DAYS,
  daysFromNow,
  getKeySchema,
  toKeyPayload,
  type KeyValues,
} from './lib/key-schema'

type KeyCreateDrawerProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onCreated: () => void
}

export function KeyCreateDrawer(props: KeyCreateDrawerProps) {
  const { t } = useTranslation()
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [created, setCreated] = useState<EsseraCreatedKey | null>(null)
  const schema = useMemo(() => getKeySchema(t), [t])
  const expiryPresets = [
    { days: 90, label: t('90 Days') },
    { days: 30, label: t('30 Days') },
    { days: 7, label: t('7 Days') },
    { days: 1, label: t('1 Day') },
  ]
  const form = useForm<KeyValues>({
    resolver: zodResolver(schema),
    defaultValues: { name: '', expiresAt: daysFromNow(KEY_LIFETIME_DAYS) },
  })

  useEffect(() => {
    if (props.open) {
      form.reset({ name: '', expiresAt: daysFromNow(KEY_LIFETIME_DAYS) })
    }
  }, [props.open, form])

  // The gateway answers in English source text that the locales translate.
  function refuse(failure: unknown) {
    const fallback = t('Failed to create API key')
    handleServerError(failure, fallback, {
      title: translateServerText(t, getServerErrorMessage(failure, fallback)),
    })
  }

  async function onSubmit(values: KeyValues) {
    setIsSubmitting(true)
    try {
      const result = await createEsseraKey(toKeyPayload(values))
      if (!result.success || !result.data) {
        refuse(result)
        return
      }
      props.onOpenChange(false)
      props.onCreated()
      setCreated(result.data)
    } catch (error) {
      refuse(error)
    } finally {
      setIsSubmitting(false)
    }
  }

  return (
    <>
      <Sheet open={props.open} onOpenChange={props.onOpenChange}>
        <SheetContent
          className={sideDrawerContentClassName('max-w-none sm:!max-w-[620px]')}
        >
          <SheetHeader className={sideDrawerHeaderClassName()}>
            <SheetTitle>{t('Create API Key')}</SheetTitle>
            <SheetDescription>
              {t('Add a new API key by providing necessary info.')}
            </SheetDescription>
          </SheetHeader>
          <Form {...form}>
            <form
              id='essera-key-form'
              onSubmit={form.handleSubmit(onSubmit)}
              inert={isSubmitting ? true : undefined}
              className={sideDrawerFormClassName('gap-5')}
            >
              <SideDrawerSection>
                <SideDrawerSectionHeader
                  title={t('Basic Information')}
                  description={t('Set API key basic information')}
                  icon={<KeyRound className='size-4' />}
                  iconTone='info'
                />
                <FormField
                  control={form.control}
                  name='name'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Name')}</FormLabel>
                      <FormControl>
                        <Input {...field} placeholder={t('Enter a name')} />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='expiresAt'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Expiration Time')}</FormLabel>
                      <div className='grid gap-2 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center'>
                        <FormControl>
                          <DateTimePicker
                            value={field.value}
                            onChange={(date) =>
                              field.onChange(
                                date ?? daysFromNow(KEY_LIFETIME_DAYS)
                              )
                            }
                            className='min-w-0 [&_input[type=time]]:w-24 sm:[&_input[type=time]]:w-32'
                          />
                        </FormControl>
                        <div className='grid grid-cols-4 gap-2 sm:flex'>
                          {expiryPresets.map((preset) => (
                            <Button
                              key={preset.days}
                              type='button'
                              variant='outline'
                              size='sm'
                              className='px-2 text-xs sm:px-3 sm:text-sm'
                              onClick={() =>
                                form.setValue(
                                  'expiresAt',
                                  daysFromNow(preset.days),
                                  { shouldValidate: true }
                                )
                              }
                            >
                              {preset.label}
                            </Button>
                          ))}
                        </div>
                      </div>
                      <FormDescription>
                        {t('A key lasts at most 90 days.')}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </SideDrawerSection>
            </form>
          </Form>
          <SheetFooter className={sideDrawerFooterClassName()}>
            <SheetClose
              render={<Button variant='outline' className='w-full sm:w-auto' />}
            >
              {t('Close')}
            </SheetClose>
            <Button
              type='submit'
              form='essera-key-form'
              disabled={isSubmitting}
              className='w-full sm:w-auto'
            >
              {isSubmitting ? t('Creating...') : t('Create')}
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>
      <KeyCreatedDialog created={created} onClose={() => setCreated(null)} />
    </>
  )
}
