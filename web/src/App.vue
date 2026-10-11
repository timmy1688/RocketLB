<script setup lang="ts">
import {
  NConfigProvider,
  NDialogProvider,
  NMessageProvider,
  darkTheme,
  dateZhCN,
  zhCN,
  type GlobalThemeOverrides,
} from "naive-ui";

const themeOverrides: GlobalThemeOverrides = {
  common: {
    primaryColor: "#3DDC97",
    primaryColorHover: "#62E4AD",
    primaryColorPressed: "#2FBF80",
    primaryColorSuppl: "#3DDC97",
    bodyColor: "#101411",
    cardColor: "#171b18",
    modalColor: "#171b18",
    popoverColor: "#1c211e",
    tableColor: "#171b18",
    borderColor: "#2a312c",
    dividerColor: "#2a312c",
    textColorBase: "#e7eee8",
    inputColor: "#101411",
  },
};
</script>

<template>
  <n-config-provider :theme="darkTheme" :theme-overrides="themeOverrides" :locale="zhCN" :date-locale="dateZhCN">
    <n-message-provider>
      <n-dialog-provider>
        <router-view />
      </n-dialog-provider>
    </n-message-provider>
  </n-config-provider>
</template>
