<template>
  <div class="space-y-8">
    <div>
      <h1
        class="text-foreground text-[1.75rem] leading-tight font-semibold tracking-tight"
      >
        个人信息
      </h1>
      <p class="text-default-500 mt-2 text-sm">
        管理 NextMoe·未萌 账号的资料与安全设置
      </p>
    </div>

    <div class="grid gap-6 lg:grid-cols-[19rem_minmax(0,1fr)] lg:items-start">
      <div class="lg:sticky lg:top-24">
        <ProfileInfo />
      </div>

      <div class="space-y-6">
        <ProfileEdit />
        <ProfileEmailChange />
        <ProfilePasswordChange />
        <ProfileMoemoepointLog />
        <ProfileDeleteAccount />
      </div>
    </div>
  </div>
</template>
