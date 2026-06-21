import { useMemo } from 'react'
import * as z from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
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
import { Switch } from '@/components/ui/switch'
import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { useUpdateOption } from '../hooks/use-update-option'

/**
 * The dotted ldap.* keys are modeled as a nested object here (like discord.* /
 * oidc.* in oauth-section) to keep form state and dirty tracking aligned with
 * react-hook-form, and flattened back to dotted server keys on save.
 */
const ldapSchema = z.object({
  ldap: z.object({
    enabled: z.boolean(),
    server_url: z.string(),
    bind_dn: z.string(),
    bind_password: z.string(),
    user_base: z.string(),
    user_filter: z.string(),
    username_attribute: z.string(),
    display_name_attribute: z.string(),
    email_attribute: z.string(),
    skip_tls_verify: z.boolean(),
    auto_register: z.boolean(),
    default_group: z.string(),
  }),
})

type LdapFormValues = z.infer<typeof ldapSchema>

type FlatLdapDefaults = {
  'ldap.enabled': boolean
  'ldap.server_url': string
  'ldap.bind_dn': string
  'ldap.bind_password': string
  'ldap.user_base': string
  'ldap.user_filter': string
  'ldap.username_attribute': string
  'ldap.display_name_attribute': string
  'ldap.email_attribute': string
  'ldap.skip_tls_verify': boolean
  'ldap.auto_register': boolean
  'ldap.default_group': string
}

type LdapSectionProps = {
  defaultValues: FlatLdapDefaults
}

const buildFormDefaults = (d: FlatLdapDefaults): LdapFormValues => ({
  ldap: {
    enabled: d['ldap.enabled'],
    server_url: d['ldap.server_url'] ?? '',
    bind_dn: d['ldap.bind_dn'] ?? '',
    bind_password: d['ldap.bind_password'] ?? '',
    user_base: d['ldap.user_base'] ?? '',
    user_filter: d['ldap.user_filter'] ?? '(uid=%s)',
    username_attribute: d['ldap.username_attribute'] ?? 'uid',
    display_name_attribute: d['ldap.display_name_attribute'] ?? 'cn',
    email_attribute: d['ldap.email_attribute'] ?? 'mail',
    skip_tls_verify: d['ldap.skip_tls_verify'],
    auto_register: d['ldap.auto_register'],
    default_group: d['ldap.default_group'] ?? 'default',
  },
})

export function LdapSection({ defaultValues }: LdapSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const formDefaults = useMemo(
    () => buildFormDefaults(defaultValues),
    [defaultValues]
  )

  const form = useForm<LdapFormValues>({
    resolver: zodResolver(ldapSchema),
    defaultValues: formDefaults,
  })

  useResetForm(form, formDefaults)

  const onSubmit = async (data: LdapFormValues) => {
    const flat = data.ldap
    const dottedKeys = Object.keys(flat) as (keyof typeof flat)[]
    for (const key of dottedKeys) {
      const serverKey = `ldap.${key}`
      const val = flat[key]
      const strVal = typeof val === 'boolean' ? String(val) : val
      await updateOption.mutateAsync({ key: serverKey, value: strVal })
    }
  }

  return (
    <SettingsSection title={t('LDAP Authentication')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
          />

          <FormField
            control={form.control}
            name='ldap.enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable LDAP Login')}</FormLabel>
                  <FormDescription>
                    {t('Allow users to authenticate via an LDAP server')}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          <FormField
            control={form.control}
            name='ldap.server_url'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Server URL')}</FormLabel>
                <FormControl>
                  <Input
                    placeholder='ldap://host:389'
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t('ldap:// or ldaps:// scheme with host and port')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='ldap.bind_dn'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Bind DN (optional)')}</FormLabel>
                <FormControl>
                  <Input
                    placeholder='cn=admin,dc=example,dc=com'
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Service account for searching users. Leave empty for simple bind mode.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='ldap.bind_password'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Bind Password')}</FormLabel>
                <FormControl>
                  <Input type='password' {...field} />
                </FormControl>
                <FormDescription>
                  {t('Password for the bind account above')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='ldap.user_base'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('User Base DN')}</FormLabel>
                <FormControl>
                  <Input
                    placeholder='ou=users,dc=example,dc=com'
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t('Base DN under which to search for user accounts')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='ldap.user_filter'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('User Filter')}</FormLabel>
                <FormControl>
                  <Input placeholder='(uid=%s)' {...field} />
                </FormControl>
                <FormDescription>
                  {t(
                    'LDAP search filter. Use %s as a placeholder for the username.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='ldap.username_attribute'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Username Attribute')}</FormLabel>
                <FormControl>
                  <Input placeholder='uid' {...field} />
                </FormControl>
                <FormDescription>
                  {t('e.g. uid for OpenLDAP, sAMAccountName for Active Directory')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='ldap.display_name_attribute'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Display Name Attribute')}</FormLabel>
                <FormControl>
                  <Input placeholder='cn' {...field} />
                </FormControl>
                <FormDescription>
                  {t('e.g. cn or displayName')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='ldap.email_attribute'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Email Attribute')}</FormLabel>
                <FormControl>
                  <Input placeholder='mail' {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='ldap.default_group'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Default Group')}</FormLabel>
                <FormControl>
                  <Input placeholder='default' {...field} />
                </FormControl>
                <FormDescription>
                  {t('User group assigned to newly provisioned LDAP users')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='ldap.skip_tls_verify'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Skip TLS Verification')}</FormLabel>
                  <FormDescription>
                    {t('Disable certificate verification (for self-signed certs)')}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          <FormField
            control={form.control}
            name='ldap.auto_register'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Auto Register Users')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Automatically create a local account on first LDAP login'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
