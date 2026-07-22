<script setup lang="ts">
import { onMounted } from 'vue'
import { ArrowRight, CircleCheck, CloudCog, KeyRound, Route, ShieldCheck, Zap } from '@lucide/vue'
import SiteFooter from '../components/SiteFooter.vue'
import { useSystemStore } from '../stores/system'

const system = useSystemStore()

function resetLogo(event: Event) {
  ;(event.target as HTMLImageElement).src = '/aiferry-logo.png'
}

onMounted(() => { void system.load().catch(() => undefined) })
</script>

<template>
  <main class="home-page">
    <header class="home-header">
      <RouterLink class="home-brand" to="/" :aria-label="system.systemName">
        <span class="home-brand-mark"><img :src="system.logoUrl" alt="" @error="resetLogo" /></span>
        <strong>{{ system.systemName }}</strong>
      </RouterLink>
      <nav class="home-nav" aria-label="主导航">
        <a href="#capabilities">能力</a>
        <a href="#architecture">架构</a>
        <RouterLink class="home-login" :to="{ name: 'login', query: { returnTo: '/dashboard' } }">登录控制台 <ArrowRight :size="15" /></RouterLink>
      </nav>
    </header>

    <section class="home-hero" aria-labelledby="home-title">
      <div class="home-hero-copy">
        <p class="home-eyebrow"><span /> AI ROUTING, MADE LEGIBLE</p>
        <h1 id="home-title">让每一次 AI 调用，都走在清晰的航线上。</h1>
        <p class="home-lede">{{ system.systemName }} 将模型、渠道、密钥与用量收束在一处。按你的规则，稳定地抵达每个上游服务。</p>
        <div class="home-actions">
          <RouterLink class="home-primary-action" :to="{ name: 'login', query: { returnTo: '/dashboard' } }">进入控制台 <ArrowRight :size="18" /></RouterLink>
          <a class="home-text-action" href="#architecture">查看路由方式</a>
        </div>
        <dl class="home-signals" aria-label="核心能力">
          <div><dt>多模型</dt><dd>统一 API 接入</dd></div>
          <div><dt>可观测</dt><dd>请求与成本明细</dd></div>
          <div><dt>可恢复</dt><dd>健康检查与切换</dd></div>
        </dl>
      </div>

      <div class="route-console" aria-label="模型路由示意图">
        <div class="console-topline"><span>ROUTE MAP</span><span class="console-live"><i /> LIVE</span></div>
        <div class="console-request"><span class="console-dot" /><span>api.your-domain.com</span><b>/v1/chat/completions</b></div>
        <div class="console-router"><span class="router-orbit orbit-a" /><span class="router-orbit orbit-b" /><img :src="system.logoUrl" alt="" @error="resetLogo" /></div>
        <div class="console-route-paths" aria-hidden="true"><i /><i /><i /></div>
        <div class="console-destinations">
          <div class="destination destination-openai"><span class="destination-mark">O</span><span><b>GPT</b><small>优先路由</small></span><CircleCheck :size="17" /></div>
          <div class="destination destination-anthropic"><span class="destination-mark">A</span><span><b>Claude</b><small>备用路由</small></span><CircleCheck :size="17" /></div>
          <div class="destination destination-local"><span class="destination-mark">L</span><span><b>Local LLM</b><small>内网模型</small></span><CircleCheck :size="17" /></div>
        </div>
        <div class="console-footer"><span><i /> 可用渠道 3</span><span>延迟 · 低</span></div>
      </div>
    </section>

    <section id="capabilities" class="home-capabilities" aria-labelledby="capabilities-title">
      <div class="section-intro"><p>你掌控的，不只是一个地址</p><h2 id="capabilities-title">把模型服务做成一套可靠的基础设施。</h2></div>
      <div class="capability-grid">
        <article><span class="capability-icon"><Route :size="22" /></span><h3>按规则抵达</h3><p>为不同模型定义优先级与权重，渠道异常时自动切换，调用方无需感知上游变化。</p></article>
        <article><span class="capability-icon"><KeyRound :size="22" /></span><h3>密钥各司其职</h3><p>为团队与服务签发访问密钥，明确用量边界，让每一笔调用都有归属。</p></article>
        <article><span class="capability-icon"><CloudCog :size="22" /></span><h3>运行一目了然</h3><p>在控制台查看模型、渠道、用量与估算成本，快速判断系统当前的运行状态。</p></article>
      </div>
    </section>

    <section id="architecture" class="home-architecture" aria-labelledby="architecture-title">
      <div class="architecture-copy"><p class="home-eyebrow"><span /> ONE ENTRANCE, MANY DESTINATIONS</p><h2 id="architecture-title">一个入口，服务所有模型。</h2><p>应用只需连接一次；渠道配置、模型价格和故障处理则留在 {{ system.systemName }} 中持续演进。</p><RouterLink :to="{ name: 'login', query: { returnTo: '/dashboard' } }" class="architecture-link">打开控制台 <ArrowRight :size="16" /></RouterLink></div>
      <ol class="architecture-flow">
        <li><span>01</span><div><b>应用发起请求</b><small>兼容的 API 地址与认证方式</small></div></li>
        <li><span>02</span><div><b>网关选择航线</b><small>按模型、优先级和可用性决策</small></div></li>
        <li><span>03</span><div><b>上游完成响应</b><small>记录请求、延迟和成本</small></div></li>
      </ol>
    </section>

    <section class="home-closing" aria-label="进入控制台">
      <Zap :size="24" aria-hidden="true" />
      <p>从一条清晰的航线开始。</p>
      <RouterLink :to="{ name: 'login', query: { returnTo: '/dashboard' } }">进入 {{ system.systemName }} <ArrowRight :size="17" /></RouterLink>
    </section>

    <SiteFooter />
  </main>
</template>

<style scoped>
.home-page { min-height: 100vh; overflow: hidden; color: #132d45; background: #f1f5f9; }
.home-header, .home-hero, .home-capabilities, .home-architecture { width: min(1180px, calc(100% - 56px)); margin: 0 auto; }
.home-header { display: flex; min-height: 80px; align-items: center; justify-content: space-between; gap: 24px; }
.home-brand, .home-nav, .home-login, .home-actions, .home-primary-action, .home-text-action, .architecture-link, .home-closing a { display: flex; align-items: center; }
.home-brand { gap: 10px; color: inherit; text-decoration: none; }.home-brand strong { font-size: 19px; font-weight: 720; }.home-brand-mark { display: grid; width: 37px; height: 37px; place-items: center; overflow: hidden; border: 1px solid #afc3d8; border-radius: 5px; background: #fff; }.home-brand-mark img { width: 29px; height: 29px; object-fit: contain; }
.home-nav { gap: 25px; }.home-nav > a:not(.home-login) { color: #586f82; font-size: 13px; text-decoration: none; }.home-nav > a:not(.home-login):hover { color: #1c67a4; }.home-login { gap: 7px; padding: 9px 12px; border: 1px solid #b9c9db; border-radius: 5px; color: #183d5c; background: #fbfdff; font-size: 13px; font-weight: 650; text-decoration: none; }.home-login:hover { border-color: #1c67a4; color: #164f80; }
.home-hero { display: grid; grid-template-columns: minmax(0, .9fr) minmax(460px, 1.1fr); align-items: center; gap: clamp(44px, 7vw, 112px); min-height: calc(100vh - 148px); padding: 72px 0 0; }
.home-eyebrow { display: flex; align-items: center; gap: 9px; margin: 0 0 20px; color: #56728b; font-family: 'JetBrains Mono', monospace; font-size: 11px; font-weight: 500; }.home-eyebrow span { width: 28px; height: 1px; background: #e87651; }
.home-hero h1, .section-intro h2, .architecture-copy h2 { margin: 0; font-weight: 740; line-height: 1.08; }.home-hero h1 { max-width: 620px; color: #132f4b; font-size: clamp(47px, 5vw, 74px); }.home-lede { max-width: 520px; margin: 26px 0 0; color: #587083; font-size: 17px; line-height: 1.8; }
.home-actions { flex-wrap: wrap; gap: 22px; margin-top: 34px; }.home-primary-action { gap: 10px; min-height: 48px; padding: 0 18px; border-radius: 5px; color: #fff; background: #1d609b; box-shadow: 0 8px 0 #104676; font-weight: 700; text-decoration: none; transition: transform 160ms ease, box-shadow 160ms ease; }.home-primary-action:hover { transform: translateY(2px); box-shadow: 0 6px 0 #104676; }.home-text-action { color: #284f71; font-size: 14px; font-weight: 650; text-decoration: underline; text-underline-offset: 4px; }.home-signals { display: flex; flex-wrap: wrap; gap: 20px 32px; margin: 48px 0 0; }.home-signals div { min-width: 93px; }.home-signals dt { color: #216fb0; font-family: 'JetBrains Mono', monospace; font-size: 12px; font-weight: 600; }.home-signals dd { margin: 6px 0 0; color: #65798b; font-size: 12px; }
.route-console { position: relative; min-height: 470px; padding: 20px; overflow: hidden; border: 1px solid #aabfd5; border-radius: 7px; background: #fcfdff; box-shadow: 16px 18px 0 #d5e1ee; }.route-console::before { position: absolute; inset: 58px 20px 56px; border: 1px dashed #cfdae7; content: ''; pointer-events: none; }.console-topline, .console-request, .console-footer { position: relative; z-index: 1; display: flex; align-items: center; justify-content: space-between; }.console-topline { color: #60788d; font-family: 'JetBrains Mono', monospace; font-size: 10px; }.console-live { display: inline-flex; align-items: center; gap: 6px; color: #2474b2; }.console-live i, .console-footer i { width: 7px; height: 7px; border-radius: 50%; background: #2b84c0; }.console-request { margin: 26px auto 0; padding: 12px 14px; border: 1px solid #c0cedd; border-radius: 4px; background: #fff; box-shadow: 0 5px 12px rgb(33 73 112 / 7%); color: #35536d; font-family: 'JetBrains Mono', monospace; font-size: 11px; }.console-request b { color: #256ca7; font-weight: 600; }.console-dot { width: 8px; height: 8px; border-radius: 50%; background: #e87651; }.console-router { position: absolute; top: 50%; left: 29%; z-index: 1; display: grid; width: 70px; height: 70px; place-items: center; border: 1px solid #78a7ce; border-radius: 50%; background: #e7f0fa; box-shadow: 0 0 0 10px #f4f8fd; transform: translateY(-24%); }.console-router img { width: 42px; height: 42px; object-fit: contain; }.router-orbit { position: absolute; border-radius: 50%; border: 1px solid #88b4dc; }.orbit-a { inset: -8px; border-right-color: transparent; transform: rotate(-20deg); }.orbit-b { inset: -15px; border-left-color: transparent; opacity: .55; transform: rotate(40deg); }.console-route-paths { position: absolute; top: 130px; right: 196px; left: calc(29% + 65px); z-index: 0; height: 230px; }.console-route-paths i { position: absolute; top: 123px; left: 0; width: 100%; height: 1px; background: #9eb7d2; transform-origin: left center; }.console-route-paths i:nth-child(1) { transform: rotate(-38deg); }.console-route-paths i:nth-child(2) { transform: rotate(-24deg); }.console-destinations { position: absolute; top: 105px; right: 24px; z-index: 2; display: grid; gap: 13px; }.destination { display: flex; width: 188px; align-items: center; gap: 10px; padding: 10px; border: 1px solid #c9d5e3; border-radius: 4px; background: #fff; box-shadow: 0 4px 10px rgb(33 73 112 / 5%); }.destination > span:nth-child(2) { display: grid; flex: 1; gap: 2px; }.destination b { color: #1d405e; font-size: 12px; }.destination small { color: #718597; font-size: 10px; }.destination > svg { color: #277abb; }.destination-mark { display: grid; width: 25px; height: 25px; place-items: center; border-radius: 4px; font-family: 'JetBrains Mono', monospace; font-size: 12px; font-weight: 700; }.destination-openai .destination-mark { color: #236da8; background: #e6f0fa; }.destination-anthropic .destination-mark { color: #a75538; background: #fbe9e2; }.destination-local .destination-mark { color: #3263aa; background: #e5edfb; }.console-footer { position: absolute; right: 20px; bottom: 17px; left: 20px; padding-top: 14px; border-top: 1px solid #d9e3ee; color: #6a8092; font-family: 'JetBrains Mono', monospace; font-size: 10px; }.console-footer span { display: flex; align-items: center; gap: 6px; }
.home-capabilities { padding: 60px 0 100px; }.section-intro { display: grid; grid-template-columns: minmax(0, .72fr) minmax(0, 1.28fr); align-items: start; gap: 32px; padding-bottom: 34px; border-bottom: 1px solid #c3d0df; }.section-intro p { margin: 8px 0 0; color: #55738d; font-family: 'JetBrains Mono', monospace; font-size: 11px; }.section-intro h2, .architecture-copy h2 { max-width: 675px; font-size: clamp(31px, 3.2vw, 48px); }.capability-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); }.capability-grid article { min-height: 225px; padding: 30px 32px 22px; border-right: 1px solid #c3d0df; }.capability-grid article:first-child { padding-left: 0; }.capability-grid article:last-child { border: 0; }.capability-icon { display: grid; width: 42px; height: 42px; place-items: center; border: 1px solid #8ca9c8; border-radius: 50%; color: #286eaa; background: #e7f0fa; }.capability-grid h3 { margin: 24px 0 9px; color: #1a3a57; font-size: 18px; }.capability-grid p { max-width: 285px; margin: 0; color: #62798d; font-size: 14px; line-height: 1.75; }
.home-architecture { display: grid; grid-template-columns: 1fr 1fr; gap: 86px; padding: 86px 0; border-top: 1px solid #bdccdc; }.architecture-copy > p:not(.home-eyebrow) { max-width: 480px; margin: 20px 0 0; color: #587289; font-size: 15px; line-height: 1.8; }.architecture-link { gap: 7px; width: max-content; margin-top: 26px; color: #246da9; font-size: 14px; font-weight: 700; text-decoration: none; }.architecture-link:hover { text-decoration: underline; text-underline-offset: 4px; }.architecture-flow { display: grid; gap: 0; margin: 0; padding: 0; list-style: none; counter-reset: flow; }.architecture-flow li { display: grid; grid-template-columns: 52px 1fr; align-items: center; gap: 16px; min-height: 95px; border-top: 1px solid #cad6e2; }.architecture-flow li:last-child { border-bottom: 1px solid #cad6e2; }.architecture-flow li > span { color: #e87651; font-family: 'JetBrains Mono', monospace; font-size: 12px; }.architecture-flow div { display: grid; gap: 5px; }.architecture-flow b { color: #1d405e; font-size: 15px; }.architecture-flow small { color: #6a8194; font-size: 12px; }
.home-closing { display: flex; min-height: 170px; align-items: center; justify-content: center; gap: 17px; padding: 36px 28px; color: #f3f7fc; background: #173d61; }.home-closing > svg { color: #f28a62; }.home-closing p { margin: 0; font-size: clamp(21px, 2.3vw, 30px); font-weight: 650; }.home-closing a { gap: 8px; margin-left: 16px; padding: 11px 14px; border: 1px solid #86a7c7; border-radius: 5px; color: #fff; font-size: 13px; font-weight: 650; text-decoration: none; }.home-closing a:hover { border-color: #fff; background: rgb(255 255 255 / 8%); }
@media (prefers-reduced-motion: reduce) { .home-primary-action { transition: none; } }
@media (max-width: 900px) { .home-header, .home-hero, .home-capabilities, .home-architecture { width: min(100% - 32px, 680px); }.home-hero { grid-template-columns: 1fr; min-height: 0; padding: 64px 0 76px; }.home-hero h1 { max-width: 690px; }.route-console { width: min(100%, 620px); min-height: 455px; }.home-capabilities { padding: 78px 0; }.home-architecture { grid-template-columns: 1fr; gap: 50px; }.section-intro { grid-template-columns: 1fr; gap: 12px; }.section-intro p { margin: 0; }.capability-grid article { padding: 28px 20px; }.capability-grid article:first-child { padding-left: 0; } }
@media (max-width: 620px) { .home-header { min-height: 70px; }.home-brand strong { font-size: 17px; }.home-nav { gap: 13px; }.home-nav > a:not(.home-login) { display: none; }.home-login { padding: 8px 10px; font-size: 12px; }.home-hero { padding-top: 46px; }.home-hero h1 { font-size: 42px; }.home-lede { margin-top: 20px; font-size: 15px; }.home-signals { gap: 14px 20px; margin-top: 38px; }.route-console { min-height: 385px; padding: 16px; box-shadow: 9px 11px 0 #d5e1ee; }.route-console::before { inset: 50px 16px 48px; }.console-request { margin-top: 20px; font-size: 9px; }.console-request b { display: none; }.console-router { left: 22%; width: 58px; height: 58px; }.console-router img { width: 34px; height: 34px; }.console-route-paths { top: 99px; right: 137px; left: calc(22% + 54px); height: 184px; }.console-route-paths i { top: 113px; }.console-route-paths i:nth-child(1) { transform: rotate(-40deg); }.console-route-paths i:nth-child(2) { transform: rotate(-24deg); }.console-destinations { top: 89px; right: 12px; gap: 8px; }.destination { width: 142px; gap: 7px; padding: 7px; }.destination-mark { width: 22px; height: 22px; }.destination b { font-size: 10px; }.destination small { font-size: 9px; }.destination > svg { width: 14px; }.console-footer { right: 16px; bottom: 13px; left: 16px; font-size: 9px; }.home-capabilities { padding: 66px 0; }.capability-grid { grid-template-columns: 1fr; }.capability-grid article, .capability-grid article:first-child { min-height: 0; padding: 24px 0; border-right: 0; border-bottom: 1px solid #c3d0df; }.capability-grid article:last-child { border-bottom: 0; }.capability-grid h3 { margin-top: 15px; }.home-architecture { padding: 64px 0; }.home-closing { min-height: 0; flex-wrap: wrap; justify-content: flex-start; gap: 12px; padding: 35px 16px; }.home-closing p { flex: 1 1 calc(100% - 42px); }.home-closing a { margin-left: 0; } }
</style>
