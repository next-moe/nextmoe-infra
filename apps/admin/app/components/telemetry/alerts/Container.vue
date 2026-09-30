<template>
  <div class="space-y-8">
    <h1 class="text-foreground text-2xl font-bold">告警</h1>
    <section class="space-y-3">
      <h2 class="text-foreground text-lg font-semibold">渠道</h2>
      <TelemetryAlertsChannels />
    </section>
    <section class="space-y-3">
      <h2 class="text-foreground text-lg font-semibold">记录</h2>
      <TelemetryAlertsHistory />
    </section>
  </div>
</template>
