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
import { ArrowRight01Icon, Wallet01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { useSystemConfig } from '@/hooks/use-system-config'

interface HeroProps {
  className?: string
  isAuthenticated?: boolean
}

const MODEL_SIGNALS = ['OpenAI', 'Claude', 'Gemini', 'DeepSeek'] as const
const GRID_CELLS = Array.from({ length: 20 }, (_, index) => index)

export function Hero(props: HeroProps) {
  const { t } = useTranslation()
  const { systemName, logo } = useSystemConfig()
  // 账号由管理员创建，访客统一从「开始使用」进入登录页
  const primaryDestination = props.isAuthenticated ? '/wallet' : '/sign-in'
  const primaryLabel = props.isAuthenticated
    ? t('Open wallet')
    : t('Get Started')

  return (
    <section className='niu-dali-hero relative isolate min-h-[min(48rem,calc(100svh-5rem))] overflow-hidden px-5 pt-24 pb-16 sm:px-8 md:pt-28 lg:px-10'>
      <div aria-hidden='true' className='absolute inset-0 overflow-hidden'>
        <div className='absolute inset-y-0 right-0 grid w-full grid-cols-4 grid-rows-5 opacity-35 sm:w-4/5 lg:w-[58%]'>
          {GRID_CELLS.map((cell) => (
            <span className='niu-dali-grid-cell' key={cell} />
          ))}
        </div>

        <img
          src={logo}
          alt=''
          className='absolute top-1/2 right-[8%] size-64 -translate-y-1/2 opacity-[0.08] sm:size-80 lg:size-[28rem] lg:opacity-[0.16]'
        />

        <div className='absolute top-[28%] right-[7%] hidden w-72 gap-2 lg:grid'>
          {MODEL_SIGNALS.map((model, index) => (
            <div
              className='niu-dali-signal flex h-11 items-center justify-between border px-3 text-xs'
              key={model}
              style={{ animationDelay: `${index * 500}ms` }}
            >
              <span className='font-medium'>{model}</span>
              <span className='flex items-center gap-2 opacity-70'>
                <span className='niu-dali-status-dot size-1.5 rounded-full' />
                API
              </span>
            </div>
          ))}
        </div>
      </div>

      <div className='relative z-10 mx-auto flex min-h-[min(38rem,calc(100svh-11rem))] max-w-6xl items-center'>
        <div className='max-w-2xl'>
          <div className='niu-dali-kicker mb-8 inline-flex items-center gap-3 border px-3 py-2'>
            <img src={logo} alt='' className='size-7' />
            <span className='text-xs font-semibold'>Niu Dali</span>
            <span
              aria-hidden='true'
              className='h-4 w-px bg-current opacity-25'
            />
            <span className='text-xs opacity-70'>{t('Token access')}</span>
          </div>

          <p className='niu-dali-eyebrow mb-4 text-sm font-medium'>
            {t('Token access, made straightforward')}
          </p>
          <h1 className='max-w-xl text-5xl leading-none font-semibold text-balance [overflow-wrap:anywhere] sm:text-6xl lg:text-7xl'>
            {systemName}
          </h1>
          <p className='mt-6 max-w-xl text-xl leading-8 font-medium text-balance sm:text-2xl'>
            {t('One balance for the AI models you use.')}
          </p>
          <p className='niu-dali-hero-muted mt-4 max-w-xl text-sm leading-7 sm:text-base'>
            {t(
              'Buy flexible credit, create an API key, and see every request in one place.'
            )}
          </p>

          <div className='mt-8 flex flex-wrap items-center gap-3'>
            <Button
              size='lg'
              className='niu-dali-primary-action h-11 px-5'
              render={<Link to={primaryDestination} />}
            >
              <HugeiconsIcon icon={Wallet01Icon} data-icon='inline-start' />
              {primaryLabel}
              <HugeiconsIcon icon={ArrowRight01Icon} data-icon='inline-end' />
            </Button>
            <Button
              size='lg'
              variant='outline'
              className='niu-dali-secondary-action h-11 px-5'
              render={<Link to='/pricing' />}
            >
              {t('View Pricing')}
            </Button>
          </div>

          <div className='niu-dali-hero-muted mt-10 flex flex-wrap gap-x-6 gap-y-3 text-xs'>
            <span>{t('Pay as you go')}</span>
            <span>{t('Clear model pricing')}</span>
            <span>{t('Request-level usage records')}</span>
          </div>
        </div>
      </div>
    </section>
  )
}
