<template>
  <div class="space-y-8">
    <div>
      <h1
        class="text-foreground text-[1.75rem] leading-tight font-semibold tracking-tight"
      >
        设置
      </h1>
      <p class="text-default-500 mt-2 text-sm">
        管理成人向内容的显示方式，以及各应用替你保存的偏好数据
      </p>
    </div>

    <div class="space-y-6">
      <SettingsContentRating />
      <SettingsAppData />
    </div>
  </div>
</template>
