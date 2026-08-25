/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { zodResolver } from '@hookform/resolvers/zod'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import { StatusBadge } from '@/components/status-badge'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from '@/components/ui/alert-dialog'
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
import { Progress } from '@/components/ui/progress'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'
import { api } from '@/lib/api'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import { safeNumberFieldProps } from '../utils/numeric-field'

/**
 * IMPORTANT: react-hook-form 7 interprets dotted `name` strings as nested
 * paths. If we declare the schema with literal flat keys like
 * `'performance_setting.disk_cache_enabled'`, the form state diverges from
 * what zod validates and saves silently turn into no-ops. So we model the
 * form internally with proper nested objects and only flatten back to the
 * server-side key format right before persisting.
 */
const perfSchema = z.object({
  performance_setting: z.object({
    disk_cache_enabled: z.boolean(),
    disk_cache_threshold_mb: z.coerce.number().min(1),
    disk_cache_max_size_mb: z.coerce.number().min(100),
    disk_cache_path: z.string(),
    monitor_enabled: z.boolean(),
    monitor_cpu_threshold: z.coerce.number().min(0),
    monitor_memory_threshold: z.coerce.number().min(0).max(100),
    monitor_disk_threshold: z.coerce.number().min(0).max(100),
  }),
  generated_image_storage: z.object({
    enabled: z.boolean(),
    provider: z.string(),
    credential_mode: z.enum(['env', 'ecs_ram_role']),
    ecs_ram_role_name: z.string(),
    bucket: z.string(),
    region: z.string(),
    internal_endpoint: z.string(),
    external_endpoint: z.string(),
    public_base_url: z.string(),
    presign_enabled: z.boolean(),
    presign_ttl_seconds: z.coerce.number().min(1).max(604800),
    object_prefix: z.string(),
    threshold_mb: z.coerce.number().min(1),
    max_image_mb: z.coerce.number().min(1),
    max_total_mb: z.coerce.number().min(1),
    max_upload_concurrency: z.coerce.number().min(1),
    upload_timeout_seconds: z.coerce.number().min(1).max(600),
    failure_policy: z.enum(['fallback_inline', 'fail_request']),
  }),
})

type PerfFormInput = z.input<typeof perfSchema>
type PerfFormValues = z.output<typeof perfSchema>

type FlatPerfDefaults = {
  'performance_setting.disk_cache_enabled': boolean
  'performance_setting.disk_cache_threshold_mb': number
  'performance_setting.disk_cache_max_size_mb': number
  'performance_setting.disk_cache_path': string
  'performance_setting.monitor_enabled': boolean
  'performance_setting.monitor_cpu_threshold': number
  'performance_setting.monitor_memory_threshold': number
  'performance_setting.monitor_disk_threshold': number
  'generated_image_storage.enabled': boolean
  'generated_image_storage.provider': string
  'generated_image_storage.credential_mode': 'env' | 'ecs_ram_role'
  'generated_image_storage.ecs_ram_role_name': string
  'generated_image_storage.bucket': string
  'generated_image_storage.region': string
  'generated_image_storage.internal_endpoint': string
  'generated_image_storage.external_endpoint': string
  'generated_image_storage.public_base_url': string
  'generated_image_storage.presign_enabled': boolean
  'generated_image_storage.presign_ttl_seconds': number
  'generated_image_storage.object_prefix': string
  'generated_image_storage.threshold_mb': number
  'generated_image_storage.max_image_mb': number
  'generated_image_storage.max_total_mb': number
  'generated_image_storage.max_upload_concurrency': number
  'generated_image_storage.upload_timeout_seconds': number
  'generated_image_storage.failure_policy': 'fallback_inline' | 'fail_request'
}

const buildFormDefaults = (defaults: FlatPerfDefaults): PerfFormInput => ({
  performance_setting: {
    disk_cache_enabled: defaults['performance_setting.disk_cache_enabled'],
    disk_cache_threshold_mb:
      defaults['performance_setting.disk_cache_threshold_mb'],
    disk_cache_max_size_mb:
      defaults['performance_setting.disk_cache_max_size_mb'],
    disk_cache_path: defaults['performance_setting.disk_cache_path'] ?? '',
    monitor_enabled: defaults['performance_setting.monitor_enabled'],
    monitor_cpu_threshold:
      defaults['performance_setting.monitor_cpu_threshold'],
    monitor_memory_threshold:
      defaults['performance_setting.monitor_memory_threshold'],
    monitor_disk_threshold:
      defaults['performance_setting.monitor_disk_threshold'],
  },
  generated_image_storage: {
    enabled: defaults['generated_image_storage.enabled'],
    provider: defaults['generated_image_storage.provider'] ?? 'aliyun_oss',
    credential_mode:
      defaults['generated_image_storage.credential_mode'] ?? 'env',
    ecs_ram_role_name:
      defaults['generated_image_storage.ecs_ram_role_name'] ?? '',
    bucket: defaults['generated_image_storage.bucket'] ?? '',
    region: defaults['generated_image_storage.region'] ?? '',
    internal_endpoint:
      defaults['generated_image_storage.internal_endpoint'] ?? '',
    external_endpoint:
      defaults['generated_image_storage.external_endpoint'] ?? '',
    public_base_url: defaults['generated_image_storage.public_base_url'] ?? '',
    presign_enabled: defaults['generated_image_storage.presign_enabled'],
    presign_ttl_seconds:
      defaults['generated_image_storage.presign_ttl_seconds'],
    object_prefix:
      defaults['generated_image_storage.object_prefix'] ?? 'gemini/generated',
    threshold_mb: defaults['generated_image_storage.threshold_mb'],
    max_image_mb: defaults['generated_image_storage.max_image_mb'],
    max_total_mb: defaults['generated_image_storage.max_total_mb'],
    max_upload_concurrency:
      defaults['generated_image_storage.max_upload_concurrency'],
    upload_timeout_seconds:
      defaults['generated_image_storage.upload_timeout_seconds'],
    failure_policy:
      defaults['generated_image_storage.failure_policy'] ?? 'fallback_inline',
  },
})

const normalizeFormValues = (values: PerfFormValues): FlatPerfDefaults => ({
  'performance_setting.disk_cache_enabled':
    values.performance_setting.disk_cache_enabled,
  'performance_setting.disk_cache_threshold_mb':
    values.performance_setting.disk_cache_threshold_mb,
  'performance_setting.disk_cache_max_size_mb':
    values.performance_setting.disk_cache_max_size_mb,
  'performance_setting.disk_cache_path':
    values.performance_setting.disk_cache_path ?? '',
  'performance_setting.monitor_enabled':
    values.performance_setting.monitor_enabled,
  'performance_setting.monitor_cpu_threshold':
    values.performance_setting.monitor_cpu_threshold,
  'performance_setting.monitor_memory_threshold':
    values.performance_setting.monitor_memory_threshold,
  'performance_setting.monitor_disk_threshold':
    values.performance_setting.monitor_disk_threshold,
  'generated_image_storage.enabled': values.generated_image_storage.enabled,
  'generated_image_storage.provider': values.generated_image_storage.provider,
  'generated_image_storage.credential_mode':
    values.generated_image_storage.credential_mode,
  'generated_image_storage.ecs_ram_role_name':
    values.generated_image_storage.ecs_ram_role_name ?? '',
  'generated_image_storage.bucket': values.generated_image_storage.bucket ?? '',
  'generated_image_storage.region': values.generated_image_storage.region ?? '',
  'generated_image_storage.internal_endpoint':
    values.generated_image_storage.internal_endpoint ?? '',
  'generated_image_storage.external_endpoint':
    values.generated_image_storage.external_endpoint ?? '',
  'generated_image_storage.public_base_url':
    values.generated_image_storage.public_base_url ?? '',
  'generated_image_storage.presign_enabled':
    values.generated_image_storage.presign_enabled,
  'generated_image_storage.presign_ttl_seconds':
    values.generated_image_storage.presign_ttl_seconds,
  'generated_image_storage.object_prefix':
    values.generated_image_storage.object_prefix ?? 'gemini/generated',
  'generated_image_storage.threshold_mb':
    values.generated_image_storage.threshold_mb,
  'generated_image_storage.max_image_mb':
    values.generated_image_storage.max_image_mb,
  'generated_image_storage.max_total_mb':
    values.generated_image_storage.max_total_mb,
  'generated_image_storage.max_upload_concurrency':
    values.generated_image_storage.max_upload_concurrency,
  'generated_image_storage.upload_timeout_seconds':
    values.generated_image_storage.upload_timeout_seconds,
  'generated_image_storage.failure_policy':
    values.generated_image_storage.failure_policy,
})

function formatBytes(bytes: number, decimals = 2): string {
  if (!bytes || Number.isNaN(bytes)) return '0 Bytes'
  if (bytes === 0) return '0 Bytes'
  if (bytes < 0) return `-${formatBytes(-bytes, decimals)}`
  const k = 1024
  const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(Math.abs(bytes)) / Math.log(k))
  if (i < 0 || i >= sizes.length) return `${bytes} Bytes`
  return `${Number.parseFloat((bytes / Math.pow(k, i)).toFixed(decimals))} ${
    sizes[i]
  }`
}

interface Props {
  defaultValues: FlatPerfDefaults
}

type PerformanceStats = {
  cache_stats?: {
    current_disk_usage_bytes: number
    disk_cache_max_bytes: number
    active_disk_files: number
    disk_cache_hits: number
    current_memory_usage_bytes: number
    active_memory_buffers: number
    memory_cache_hits: number
  }
  disk_space_info?: {
    total: number
    free: number
    used: number
    used_percent: number
  }
  memory_stats?: {
    alloc: number
    total_alloc: number
    sys: number
    num_gc: number
    num_goroutine: number
  }
  disk_cache_info?: {
    path: string
    file_count: number
    total_size: number
  }
  config?: {
    is_running_in_container: boolean
  }
}

export function PerformanceSection(props: Props) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const [stats, setStats] = useState<PerformanceStats | null>(null)

  const formDefaults = useMemo(
    () => buildFormDefaults(props.defaultValues),
    [props.defaultValues]
  )

  const form = useForm<PerfFormInput, unknown, PerfFormValues>({
    resolver: zodResolver(perfSchema),
    defaultValues: formDefaults,
  })

  const baselineRef = useRef<FlatPerfDefaults>(props.defaultValues)
  const baselineSerializedRef = useRef<string>(
    JSON.stringify(props.defaultValues)
  )

  useEffect(() => {
    const serialized = JSON.stringify(props.defaultValues)
    if (serialized === baselineSerializedRef.current) return
    baselineRef.current = props.defaultValues
    baselineSerializedRef.current = serialized
    form.reset(buildFormDefaults(props.defaultValues))
  }, [props.defaultValues, form])

  const fetchStats = useCallback(async () => {
    try {
      const res = await api.get('/api/performance/stats')
      if (res.data.success) setStats(res.data.data)
    } catch {
      /* ignore */
    }
  }, [])

  useEffect(() => {
    fetchStats()
  }, [fetchStats])

  const onSubmit = async (values: PerfFormValues) => {
    const normalized = normalizeFormValues(values)
    const changedKeys = (
      Object.keys(normalized) as Array<keyof FlatPerfDefaults>
    ).filter((key) => normalized[key] !== baselineRef.current[key])

    if (changedKeys.length === 0) {
      toast.info(t('No changes to save'))
      return
    }

    for (const key of changedKeys) {
      await updateOption.mutateAsync({
        key,
        value: normalized[key],
      })
    }

    baselineRef.current = normalized
    baselineSerializedRef.current = JSON.stringify(normalized)
    form.reset(buildFormDefaults(normalized))
    fetchStats()
  }

  const clearDiskCache = async () => {
    try {
      const res = await api.delete('/api/performance/disk_cache')
      if (res.data.success) {
        toast.success(t('Disk cache cleared'))
        fetchStats()
      }
    } catch {
      toast.error(t('Cleanup failed'))
    }
  }

  const resetStats = async () => {
    try {
      const res = await api.post('/api/performance/reset_stats')
      if (res.data.success) {
        toast.success(t('Statistics reset'))
        fetchStats()
      }
    } catch {
      toast.error(t('Reset failed'))
    }
  }

  const forceGC = async () => {
    try {
      const res = await api.post('/api/performance/gc')
      if (res.data.success) {
        toast.success(t('GC executed'))
        fetchStats()
      }
    } catch {
      toast.error(t('GC execution failed'))
    }
  }

  const diskEnabled = form.watch('performance_setting.disk_cache_enabled')
  const monitorEnabled = form.watch('performance_setting.monitor_enabled')
  const generatedImageStorageEnabled = form.watch(
    'generated_image_storage.enabled'
  )
  const generatedImageCredentialMode = form.watch(
    'generated_image_storage.credential_mode'
  )
  const generatedImagePresignEnabled = form.watch(
    'generated_image_storage.presign_enabled'
  )
  const maxCacheSizeRaw = form.watch(
    'performance_setting.disk_cache_max_size_mb'
  )
  const maxCacheSizeMb =
    typeof maxCacheSizeRaw === 'number'
      ? maxCacheSizeRaw
      : Number(maxCacheSizeRaw) || 0

  const lowDiskSpace =
    diskEnabled &&
    stats?.disk_space_info &&
    stats.disk_space_info.free > 0 &&
    maxCacheSizeMb > 0 &&
    stats.disk_space_info.free < maxCacheSizeMb * 1024 * 1024

  const diskCachePercent =
    stats?.cache_stats?.disk_cache_max_bytes &&
    stats.cache_stats.disk_cache_max_bytes > 0
      ? Math.round(
          (stats.cache_stats.current_disk_usage_bytes /
            stats.cache_stats.disk_cache_max_bytes) *
            100
        )
      : 0

  return (
    <SettingsSection title={t('Performance Settings')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
          />
          {/* Disk Cache Settings */}
          <div>
            <h4 className='font-medium'>{t('Disk Cache Settings')}</h4>
            <p className='text-muted-foreground mt-1 text-xs'>
              {t(
                'When enabled, large request bodies are temporarily stored on disk instead of memory, significantly reducing memory usage. SSD recommended.'
              )}
            </p>
          </div>

          <div className='grid grid-cols-1 gap-4 md:grid-cols-3'>
            <FormField
              control={form.control}
              name='performance_setting.disk_cache_enabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Enable Disk Cache')}</FormLabel>
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
              name='performance_setting.disk_cache_threshold_mb'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Disk Cache Threshold (MB)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      step={1}
                      {...safeNumberFieldProps(field)}
                      disabled={!diskEnabled}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Use disk cache when request body exceeds this size')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='performance_setting.disk_cache_max_size_mb'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Max Disk Cache Size (MB)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={100}
                      step={1}
                      {...safeNumberFieldProps(field)}
                      disabled={!diskEnabled}
                    />
                  </FormControl>
                  {stats?.disk_space_info &&
                    stats.disk_space_info.total > 0 && (
                      <FormDescription>
                        {t('Free: {{free}} / Total: {{total}}', {
                          free: formatBytes(stats.disk_space_info.free),
                          total: formatBytes(stats.disk_space_info.total),
                        })}
                      </FormDescription>
                    )}
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          {lowDiskSpace && (
            <Alert variant='destructive'>
              <AlertDescription>
                {`${t('Warning')}: ${t('Available disk space')} (${formatBytes(stats?.disk_space_info?.free ?? 0)}) ${t('is less than the configured maximum cache size')} (${maxCacheSizeMb} MB). ${t('This may cause cache failures.')}`}
              </AlertDescription>
            </Alert>
          )}

          {!stats?.config?.is_running_in_container && (
            <FormField
              control={form.control}
              name='performance_setting.disk_cache_path'
              render={({ field }) => (
                <FormItem className='max-w-md'>
                  <FormLabel>{t('Cache Directory')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder={t(
                        'Leave empty to use system temp directory'
                      )}
                      value={field.value ?? ''}
                      onChange={(event) => field.onChange(event.target.value)}
                      name={field.name}
                      onBlur={field.onBlur}
                      ref={field.ref}
                      disabled={!diskEnabled}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          )}

          <Separator />

          {/* Generated Image Object Storage */}
          <div>
            <h4 className='font-medium'>
              {t('Generated Image Object Storage')}
            </h4>
            <p className='text-muted-foreground mt-1 text-xs'>
              {t(
                'Store large Gemini inlineData and OpenAI b64_json images in object storage, returning fileData or URL links to reduce API response bandwidth.'
              )}
            </p>
          </div>

          <Alert>
            <AlertDescription>
              {t(
                'Access keys are read from environment variables or ECS RAM role credentials and are not stored in system settings.'
              )}
            </AlertDescription>
          </Alert>

          <div className='grid grid-cols-1 gap-4 md:grid-cols-4'>
            <FormField
              control={form.control}
              name='generated_image_storage.enabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Enable Generated Image Storage')}</FormLabel>
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
              name='generated_image_storage.credential_mode'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Credential Mode')}</FormLabel>
                  <FormControl>
                    <Select
                      items={[
                        { value: 'env', label: t('Environment variables') },
                        { value: 'ecs_ram_role', label: t('ECS RAM role') },
                      ]}
                      value={field.value}
                      onValueChange={field.onChange}
                      disabled={!generatedImageStorageEnabled}
                    >
                      <SelectTrigger className='w-full'>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectGroup>
                          <SelectItem value='env'>
                            {t('Environment variables')}
                          </SelectItem>
                          <SelectItem value='ecs_ram_role'>
                            {t('ECS RAM role')}
                          </SelectItem>
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='generated_image_storage.ecs_ram_role_name'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('ECS RAM Role Name')}</FormLabel>
                  <FormControl>
                    <Input
                      value={field.value ?? ''}
                      onChange={(event) => field.onChange(event.target.value)}
                      name={field.name}
                      onBlur={field.onBlur}
                      ref={field.ref}
                      disabled={
                        !generatedImageStorageEnabled ||
                        generatedImageCredentialMode !== 'ecs_ram_role'
                      }
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Required only when using ECS RAM role credentials')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='generated_image_storage.failure_policy'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Failure Policy')}</FormLabel>
                  <FormControl>
                    <Select
                      items={[
                        {
                          value: 'fallback_inline',
                          label: t('Fallback to inlineData'),
                        },
                        { value: 'fail_request', label: t('Fail request') },
                      ]}
                      value={field.value}
                      onValueChange={field.onChange}
                      disabled={!generatedImageStorageEnabled}
                    >
                      <SelectTrigger className='w-full'>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectGroup>
                          <SelectItem value='fallback_inline'>
                            {t('Fallback to inlineData')}
                          </SelectItem>
                          <SelectItem value='fail_request'>
                            {t('Fail request')}
                          </SelectItem>
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <div className='grid grid-cols-1 gap-4 md:grid-cols-3'>
            <FormField
              control={form.control}
              name='generated_image_storage.bucket'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Bucket')}</FormLabel>
                  <FormControl>
                    <Input
                      value={field.value ?? ''}
                      onChange={(event) => field.onChange(event.target.value)}
                      name={field.name}
                      onBlur={field.onBlur}
                      ref={field.ref}
                      disabled={!generatedImageStorageEnabled}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='generated_image_storage.region'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Region')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder='cn-guangzhou'
                      value={field.value ?? ''}
                      onChange={(event) => field.onChange(event.target.value)}
                      name={field.name}
                      onBlur={field.onBlur}
                      ref={field.ref}
                      disabled={!generatedImageStorageEnabled}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='generated_image_storage.object_prefix'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Object Prefix')}</FormLabel>
                  <FormControl>
                    <Input
                      value={field.value ?? ''}
                      onChange={(event) => field.onChange(event.target.value)}
                      name={field.name}
                      onBlur={field.onBlur}
                      ref={field.ref}
                      disabled={!generatedImageStorageEnabled}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <div className='grid grid-cols-1 gap-4 md:grid-cols-3'>
            <FormField
              control={form.control}
              name='generated_image_storage.internal_endpoint'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Internal Endpoint')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder='oss-cn-guangzhou-internal.aliyuncs.com'
                      value={field.value ?? ''}
                      onChange={(event) => field.onChange(event.target.value)}
                      name={field.name}
                      onBlur={field.onBlur}
                      ref={field.ref}
                      disabled={!generatedImageStorageEnabled}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Optional, derived from region when empty')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='generated_image_storage.external_endpoint'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('External Endpoint')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder='oss-cn-guangzhou.aliyuncs.com'
                      value={field.value ?? ''}
                      onChange={(event) => field.onChange(event.target.value)}
                      name={field.name}
                      onBlur={field.onBlur}
                      ref={field.ref}
                      disabled={!generatedImageStorageEnabled}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Optional, derived from region when empty')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='generated_image_storage.public_base_url'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Public Base URL')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder='https://cdn.example.com'
                      value={field.value ?? ''}
                      onChange={(event) => field.onChange(event.target.value)}
                      name={field.name}
                      onBlur={field.onBlur}
                      ref={field.ref}
                      disabled={!generatedImageStorageEnabled}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Use a CDN or public bucket URL when available; otherwise private buckets should use presigned URLs.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <div className='grid grid-cols-1 gap-4 md:grid-cols-4'>
            <FormField
              control={form.control}
              name='generated_image_storage.threshold_mb'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Threshold (MB)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      step={1}
                      {...safeNumberFieldProps(field)}
                      disabled={!generatedImageStorageEnabled}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Only images larger than this decoded size are uploaded'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='generated_image_storage.max_image_mb'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Max Image Size (MB)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      step={1}
                      {...safeNumberFieldProps(field)}
                      disabled={!generatedImageStorageEnabled}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='generated_image_storage.max_total_mb'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Max Total Size (MB)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      step={1}
                      {...safeNumberFieldProps(field)}
                      disabled={!generatedImageStorageEnabled}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='generated_image_storage.max_upload_concurrency'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Upload Concurrency')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      step={1}
                      {...safeNumberFieldProps(field)}
                      disabled={!generatedImageStorageEnabled}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <div className='grid grid-cols-1 gap-4 md:grid-cols-3'>
            <FormField
              control={form.control}
              name='generated_image_storage.upload_timeout_seconds'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Upload Timeout (seconds)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      max={600}
                      step={1}
                      {...safeNumberFieldProps(field)}
                      disabled={!generatedImageStorageEnabled}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='generated_image_storage.presign_enabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Use Presigned URLs')}</FormLabel>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                      disabled={!generatedImageStorageEnabled}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />
            <FormField
              control={form.control}
              name='generated_image_storage.presign_ttl_seconds'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Presign TTL (seconds)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      max={604800}
                      step={1}
                      {...safeNumberFieldProps(field)}
                      disabled={
                        !generatedImageStorageEnabled ||
                        !generatedImagePresignEnabled
                      }
                    />
                  </FormControl>
                  <FormDescription>{t('Up to 604800 seconds')}</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <Separator />

          {/* System Performance Monitor */}
          <div>
            <h4 className='font-medium'>
              {t('System Performance Monitoring')}
            </h4>
            <p className='text-muted-foreground mt-1 text-xs'>
              {t(
                'When performance monitoring is enabled and system resource usage exceeds the set threshold, new Relay requests will be rejected.'
              )}
            </p>
          </div>

          <div className='grid grid-cols-1 gap-4 md:grid-cols-4'>
            <FormField
              control={form.control}
              name='performance_setting.monitor_enabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Enable Performance Monitoring')}</FormLabel>
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
              name='performance_setting.monitor_cpu_threshold'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('CPU Threshold (%)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      step={1}
                      {...safeNumberFieldProps(field)}
                      disabled={!monitorEnabled}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='performance_setting.monitor_memory_threshold'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Memory Threshold (%)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      max={100}
                      step={1}
                      {...safeNumberFieldProps(field)}
                      disabled={!monitorEnabled}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='performance_setting.monitor_disk_threshold'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Disk Threshold (%)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      max={100}
                      step={1}
                      {...safeNumberFieldProps(field)}
                      disabled={!monitorEnabled}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>
        </SettingsForm>
      </Form>

      <Separator />

      {/* Performance Stats Dashboard */}
      <div className='space-y-4'>
        <div className='flex items-center gap-2'>
          <h4 className='font-medium'>{t('Performance Monitor')}</h4>
          <Button variant='outline' size='sm' onClick={fetchStats}>
            {t('Refresh Stats')}
          </Button>
          <AlertDialog>
            <AlertDialogTrigger render={<Button variant='outline' size='sm' />}>
              {t('Clean up inactive cache')}
            </AlertDialogTrigger>
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>
                  {t('Confirm cleanup of inactive disk cache?')}
                </AlertDialogTitle>
                <AlertDialogDescription>
                  {t(
                    'This will delete temporary cache files that have not been used for more than 10 minutes'
                  )}
                </AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
                <AlertDialogAction
                  variant='destructive'
                  onClick={clearDiskCache}
                >
                  {t('Confirm')}
                </AlertDialogAction>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>
          <Button variant='outline' size='sm' onClick={resetStats}>
            {t('Reset Stats')}
          </Button>
          <Button variant='outline' size='sm' onClick={forceGC}>
            {t('Run GC')}
          </Button>
        </div>

        {stats && (
          <>
            <div className='grid grid-cols-1 gap-4 md:grid-cols-2'>
              <div className='space-y-2 rounded-lg border p-4'>
                <p className='text-sm font-medium'>
                  {t('Request Body Disk Cache')}
                </p>
                <Progress value={diskCachePercent} />
                <div className='text-muted-foreground flex justify-between text-xs'>
                  <span>
                    {formatBytes(
                      stats.cache_stats?.current_disk_usage_bytes ?? 0
                    )}{' '}
                    /{' '}
                    {formatBytes(stats.cache_stats?.disk_cache_max_bytes ?? 0)}
                  </span>
                  <span>
                    {t('Active Files')}:{' '}
                    {stats.cache_stats?.active_disk_files ?? 0}
                  </span>
                </div>
                <StatusBadge variant='neutral' copyable={false}>
                  {t('Disk Hits')}: {stats.cache_stats?.disk_cache_hits ?? 0}
                </StatusBadge>
              </div>
              <div className='space-y-2 rounded-lg border p-4'>
                <p className='text-sm font-medium'>
                  {t('Request Body Memory Cache')}
                </p>
                <div className='text-muted-foreground flex justify-between text-xs'>
                  <span>
                    {t('Current Cache Size')}:{' '}
                    {formatBytes(
                      stats.cache_stats?.current_memory_usage_bytes ?? 0
                    )}
                  </span>
                  <span>
                    {t('Active Cache Count')}:{' '}
                    {stats.cache_stats?.active_memory_buffers ?? 0}
                  </span>
                </div>
                <StatusBadge variant='neutral' copyable={false}>
                  {t('Memory Hits')}:{' '}
                  {stats.cache_stats?.memory_cache_hits ?? 0}
                </StatusBadge>
              </div>
            </div>

            {stats.disk_space_info && stats.disk_space_info.total > 0 && (
              <div className='rounded-lg border p-4'>
                <p className='mb-2 text-sm font-medium'>
                  {t('Cache Directory Disk Space')}
                </p>
                <Progress
                  value={Math.round(stats.disk_space_info.used_percent)}
                />
                <div className='text-muted-foreground mt-2 flex justify-between text-xs'>
                  <span>
                    {t('Used')}: {formatBytes(stats.disk_space_info.used)}
                  </span>
                  <span>
                    {t('Available')}: {formatBytes(stats.disk_space_info.free)}
                  </span>
                  <span>
                    {t('Total')}: {formatBytes(stats.disk_space_info.total)}
                  </span>
                </div>
              </div>
            )}

            {stats.memory_stats && (
              <div className='rounded-lg border p-4'>
                <p className='mb-2 text-sm font-medium'>
                  {t('System Memory Stats')}
                </p>
                <div className='grid grid-cols-2 gap-2 text-xs md:grid-cols-5'>
                  <div>
                    <span className='text-muted-foreground'>
                      {t('Allocated Memory')}:
                    </span>{' '}
                    {formatBytes(stats.memory_stats.alloc)}
                  </div>
                  <div>
                    <span className='text-muted-foreground'>
                      {t('Total Allocated')}:
                    </span>{' '}
                    {formatBytes(stats.memory_stats.total_alloc)}
                  </div>
                  <div>
                    <span className='text-muted-foreground'>
                      {t('System Memory')}:
                    </span>{' '}
                    {formatBytes(stats.memory_stats.sys)}
                  </div>
                  <div>
                    <span className='text-muted-foreground'>
                      {t('GC Count')}:
                    </span>{' '}
                    {stats.memory_stats.num_gc}
                  </div>
                  <div>
                    <span className='text-muted-foreground'>Goroutines:</span>{' '}
                    {stats.memory_stats.num_goroutine}
                  </div>
                </div>
              </div>
            )}

            {stats.disk_cache_info && (
              <div className='rounded-lg border p-4'>
                <p className='mb-2 text-sm font-medium'>
                  {t('Cache Directory Info')}
                </p>
                <div className='grid grid-cols-3 gap-2 text-xs'>
                  <div>
                    <span className='text-muted-foreground'>
                      {t('Cache Directory')}:
                    </span>{' '}
                    <span className='font-mono'>
                      {stats.disk_cache_info.path}
                    </span>
                  </div>
                  <div>
                    <span className='text-muted-foreground'>
                      {t('Directory File Count')}:
                    </span>{' '}
                    {stats.disk_cache_info.file_count}
                  </div>
                  <div>
                    <span className='text-muted-foreground'>
                      {t('Directory Total Size')}:
                    </span>{' '}
                    {formatBytes(stats.disk_cache_info.total_size)}
                  </div>
                </div>
              </div>
            )}
          </>
        )}
      </div>
    </SettingsSection>
  )
}
