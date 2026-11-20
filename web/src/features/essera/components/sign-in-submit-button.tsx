import { ArrowRight, Loader2 } from 'lucide-react'

import { Button } from '@/components/ui/button'

/** The designs' form label: small, bold, uppercase and tracked. */
export const FIELD_LABEL =
  'text-muted-foreground text-xs font-semibold tracking-wider uppercase'

export function SignInSubmitButton(props: {
  isLoading: boolean
  disabled?: boolean
  label: string
}) {
  return (
    <Button
      type='submit'
      className='h-11 w-full justify-center gap-2 text-sm font-semibold shadow-md shadow-emerald-600/20'
      disabled={props.isLoading || props.disabled}
    >
      {props.isLoading && (
        <Loader2 className='animate-spin' aria-hidden='true' />
      )}
      {props.label}
      {!props.isLoading && <ArrowRight aria-hidden='true' />}
    </Button>
  )
}
