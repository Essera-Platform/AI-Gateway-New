import type { TFunction } from 'i18next'
import { z } from 'zod'

import type { NewEsseraPerson } from '../people/api'

export function getPersonSchema(t: TFunction) {
  return z.object({
    email: z
      .string()
      .trim()
      .min(1, t('Email is empty!'))
      .max(254)
      .email(t('Please enter a valid email address')),
    name: z.string().trim().max(200),
    externalId: z.string().trim().max(200),
  })
}

export type PersonValues = z.infer<ReturnType<typeof getPersonSchema>>

export function toNewPerson(values: PersonValues): NewEsseraPerson {
  const person: NewEsseraPerson = { email: values.email.toLowerCase() }
  if (values.name) person.name = values.name
  if (values.externalId) person.external_id = values.externalId
  return person
}
