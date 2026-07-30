<script setup lang="ts">
import { computed } from 'vue'
import type { ModelBillingMode, PublicModel } from '../api/types'
import { configuredTokenPriceItems, formatModelPrice } from '../lib/model-pricing'

type PriceProps = Pick<PublicModel,
  | 'inputPrice'
  | 'cachedInputPrice'
  | 'cacheWritePrice'
  | 'outputPrice'
  | 'imageInputPrice'
  | 'audioInputPrice'
  | 'audioOutputPrice'
  | 'requestPrice'> & { billingMode: ModelBillingMode }

const props = defineProps<PriceProps>()
const tokenPriceItems = computed(() => configuredTokenPriceItems(props))
</script>

<template>
  <div class="price-summary">
    <template v-if="billingMode === 'token'">
      <span class="price-summary__unit">USD / 1M Token</span>
      <span v-if="!tokenPriceItems.length" class="price-summary__empty">未设置</span>
      <div v-else class="price-summary__items">
        <span v-for="item in tokenPriceItems" :key="item.label" class="price-summary__item">
          <span>{{ item.label }}</span>
          <strong>{{ formatModelPrice(item.value) }}</strong>
        </span>
      </div>
    </template>
    <template v-else-if="billingMode === 'request'">
      <span class="price-summary__unit">USD / 请求</span>
      <span v-if="typeof requestPrice !== 'number'" class="price-summary__empty">未设置</span>
      <strong v-else class="price-summary__request">{{ formatModelPrice(requestPrice) }}</strong>
    </template>
    <span v-else class="price-summary__rules">高级计费规则</span>
  </div>
</template>

<style scoped>
.price-summary { display: flex; align-items: baseline; flex-wrap: wrap; gap: 5px 10px; min-width: 0; color: #4b5763; line-height: 1.55; }.price-summary__unit { color: #7b8792; font-size: 11px; white-space: nowrap; }.price-summary__items { display: flex; flex-wrap: wrap; gap: 4px 12px; min-width: 0; }.price-summary__item { display: inline-flex; align-items: baseline; gap: 5px; min-width: 0; padding-left: 7px; border-left: 2px solid #dbe2e8; white-space: nowrap; }.price-summary__item span { color: #64717d; font-size: 12px; }.price-summary__item strong, .price-summary__request { color: #1c2b36; font-family: 'JetBrains Mono', monospace; font-size: 13px; font-weight: 600; }.price-summary__empty, .price-summary__rules { color: #7b8792; font-size: 12px; }
</style>
