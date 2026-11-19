import { Loader2, LogIn } from 'lucide-react'

import { Button } from '@/components/ui/button'

export function SignInSubmitButton(props: {
  isLoading: boolean
  disabled?: boolean
  label: string
}) {
  return (
    <Button
      type='submit'
      className='mt-2 w-full justify-center gap-2'
      disabled={props.isLoading || props.disabled}
    >
      {props.isLoading ? <Loader2 className='animate-spin' /> : <LogIn />}
      {props.label}
    </Button>
  )
}
