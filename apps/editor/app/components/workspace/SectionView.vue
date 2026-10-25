<script setup lang="ts">
import type { Component } from 'vue'
import type { WorkspaceSectionDef } from '~/constants/workspace'
import WorkBasic from './sections/work/Basic.vue'
import WorkTitles from './sections/work/Titles.vue'
import WorkIntros from './sections/work/Intros.vue'
import WorkTags from './sections/work/Tags.vue'
import WorkLinks from './sections/work/Links.vue'
import WorkCovers from './sections/work/Covers.vue'
import WorkScreenshots from './sections/work/Screenshots.vue'
import WorkRoster from './sections/work/Roster.vue'
import WorkCredits from './sections/work/Credits.vue'
import WorkCompanies from './sections/work/Companies.vue'
import WorkEngines from './sections/work/Engines.vue'
import WorkSeries from './sections/work/Series.vue'
import WorkRelations from './sections/work/Relations.vue'
import WorkReleases from './sections/work/Releases.vue'
import WorkWalkthroughs from './sections/work/Walkthroughs.vue'
import GenericSection from './GenericSection.vue'

const VIEWS: Record<string, Component> = {
  'work:basic': WorkBasic,
  'work:titles': WorkTitles,
  'work:intros': WorkIntros,
  'work:tags': WorkTags,
  'work:links': WorkLinks,
  'work:covers': WorkCovers,
  'work:screenshots': WorkScreenshots,
  'work:roster': WorkRoster,
  'work:credits': WorkCredits,
  'work:companies': WorkCompanies,
  'work:engines': WorkEngines,
  'work:series': WorkSeries,
  'work:relations': WorkRelations,
  'work:releases': WorkReleases,
  'work:walkthroughs': WorkWalkthroughs
}

const props = defineProps<{ section: WorkspaceSectionDef }>()

const view = computed(
  () => (props.section.view && VIEWS[props.section.view]) || GenericSection
)
</script>

<template>
  <component :is="view" :key="props.section.key" :section="props.section" />
</template>
