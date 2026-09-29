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
import { ArrowRight01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { AnimateInView } from '@/components/animate-in-view'
import { Button } from '@/components/ui/button'
import { useSystemConfig } from '@/hooks/use-system-config'

interface CTAProps {
  className?: string
  isAuthenticated?: boolean
}

export function CTA(props: CTAProps) {
  const { t } = useTranslation()
  const { logo } = useSystemConfig()
  // 与首屏按钮一致：访客从「开始使用」进入登录页
  const primaryDestination = props.isAuthenticated ? '/wallet' : '/sign-in'
  const primaryLabel = props.isAuthenticated
    ? t('Open wallet')
    : t('Get Started')

  return (
    <section className='niu-dali-cta relative isolate overflow-hidden px-5 py-20 sm:px-8 md:py-24 lg:px-10'>
      <img
        src={logo}
        alt=''
        aria-hidden='true'
        className='absolute top-1/2 right-[8%] -z-10 size-64 -translate-y-1/2 opacity-[0.08] md:size-80'
      />
      <AnimateInView className='mx-auto flex max-w-6xl flex-col justify-between gap-10 md:flex-row md:items-end'>
        <div className='max-w-2xl'>
          <p className='niu-dali-eyebrow text-sm font-medium'>Niu Dali</p>
          <h2 className='mt-4 text-3xl leading-tight font-semibold text-balance md:text-5xl'>
            {t('Ready for your next request?')}
          </h2>
          <p className='niu-dali-hero-muted mt-4 max-w-xl text-sm leading-7 sm:text-base'>
            {t(
              'Start small, validate your workflow, and scale when the numbers make sense.'
            )}
          </p>
        </div>

        <div className='flex shrink-0 flex-wrap gap-3'>
          <Button
            size='lg'
            className='niu-dali-primary-action h-11 px-5'
            render={<Link to={primaryDestination} />}
          >
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
      </AnimateInView>
    </section>
  )
}
