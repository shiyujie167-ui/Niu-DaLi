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
import {
  LinkSquare01Icon,
  Shield01Icon,
  Wallet01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'

import { AnimateInView } from '@/components/animate-in-view'
import { Badge } from '@/components/ui/badge'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'

interface FeaturesProps {
  className?: string
}

export function Features(_props: FeaturesProps) {
  const { t } = useTranslation()
  const features = [
    {
      id: 'pricing',
      icon: Wallet01Icon,
      title: t('Clear before you buy'),
      description: t(
        'Compare model access and pricing before you commit balance.'
      ),
      preview: (
        <div className='border-border bg-muted/30 flex items-center justify-between border px-3 py-3'>
          <div>
            <p className='text-xs font-medium'>{t('Balance')}</p>
            <p className='text-muted-foreground mt-1 text-xs'>
              {t('Ready for API usage')}
            </p>
          </div>
          <Badge variant='secondary'>{t('Pay as you go')}</Badge>
        </div>
      ),
    },
    {
      id: 'routes',
      icon: LinkSquare01Icon,
      title: t('One key, more models'),
      description: t(
        'Supports one-click configuration and perfectly adapts to NewAPI multi-protocol configuration.'
      ),
      preview: (
        <div className='flex flex-col gap-3'>
          <div className='border-border bg-muted/30 border px-3 py-2.5 font-mono text-xs'>
            nd_live_••••••••••••
          </div>
          <div className='flex flex-wrap gap-2'>
            {['OpenAI', 'Claude', 'Gemini', 'DeepSeek'].map((model) => (
              <Badge key={model} variant='outline'>
                {model}
              </Badge>
            ))}
          </div>
        </div>
      ),
    },
    {
      id: 'records',
      icon: Shield01Icon,
      title: t('Usage you can audit'),
      description: t(
        'See token usage, cost, and remaining balance in one console.'
      ),
      preview: (
        <div className='border-border divide-border grid grid-cols-2 divide-x border'>
          <div className='px-3 py-3'>
            <p className='text-xs font-medium'>{t('Usage')}</p>
            <p className='text-muted-foreground mt-1 text-xs'>Token</p>
          </div>
          <div className='px-3 py-3'>
            <p className='text-xs font-medium'>{t('Request records')}</p>
            <p className='text-muted-foreground mt-1 text-xs'>API</p>
          </div>
        </div>
      ),
    },
  ]

  return (
    <section className='bg-background px-5 py-20 sm:px-8 md:py-28 lg:px-10'>
      <div className='mx-auto max-w-6xl'>
        <AnimateInView className='max-w-2xl'>
          <p className='text-primary text-sm font-medium'>Niu Dali</p>
          <h2 className='mt-3 text-3xl leading-tight font-semibold text-balance md:text-4xl'>
            {t('Built for buying, using, and understanding tokens')}
          </h2>
          <p className='text-muted-foreground mt-4 max-w-xl text-sm leading-7 sm:text-base'>
            {t(
              'A calmer way to manage AI spend from the first top-up to the latest request.'
            )}
          </p>
        </AnimateInView>

        <div className='mt-12 grid gap-4 lg:grid-cols-3'>
          {features.map((feature, index) => (
            <AnimateInView delay={index * 90} key={feature.id}>
              <Card className='h-full rounded-lg'>
                <CardHeader>
                  <div className='bg-primary/10 text-primary mb-3 flex size-9 items-center justify-center rounded-md'>
                    <HugeiconsIcon
                      icon={feature.icon}
                      className='size-5'
                      strokeWidth={1.8}
                      aria-hidden='true'
                    />
                  </div>
                  <CardTitle className='text-lg'>{feature.title}</CardTitle>
                  <CardDescription className='min-h-12 leading-6'>
                    {feature.description}
                  </CardDescription>
                </CardHeader>
                <CardContent>{feature.preview}</CardContent>
              </Card>
            </AnimateInView>
          ))}
        </div>
      </div>
    </section>
  )
}
