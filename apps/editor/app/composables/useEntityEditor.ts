import type {
  CatalogEditCapabilities,
  CatalogFieldCapability,
  CatalogObjectSchema,
  CatalogProposal,
  CatalogSchemaField
} from '#shared/types/catalog'
import type {
  CatalogSnapshot,
  EditEntityConfig,
  EditFieldValues,
  EditIssue,
  EditStoredDraft
} from '#shared/types/edit'
import type { EditQuota } from '#shared/types/quota'

const DRAFT_SAVE_DELAY_MS = 800
const QUOTA_MAX_AGE_MS = 15_000

const schemaRequests = new Map<string, Promise<CatalogObjectSchema>>()
let quotaRequest: { at: number; promise: Promise<EditQuota | null> } | null =
  null

const readStorage = (key: string): EditStoredDraft | null => {
  try {
    const raw = localStorage.getItem(key)
    return raw ? (JSON.parse(raw) as EditStoredDraft) : null
  } catch {
    return null
  }
}

const writeStorage = (key: string, value: EditStoredDraft | null) => {
  try {
    if (value) {
      localStorage.setItem(key, JSON.stringify(value))
    } else {
      localStorage.removeItem(key)
    }
  } catch {
    return
  }
}

export const useEntityEditor = (
  config: EditEntityConfig,
  entityId: MaybeRefOrGetter<string>,
  options: { persist?: boolean } = {}
) => {
  const persist = options.persist ?? true
  const api = useEditorApi()
  const vocabularies = useVocabularies()

  const schema = ref<CatalogObjectSchema | null>(null)
  const capabilities = ref<CatalogEditCapabilities | null>(null)
  const quota = ref<EditQuota | null>(null)
  const base = ref<EditFieldValues>({})
  const draft = ref<EditFieldValues>({})
  const loading = ref(true)
  const loadError = ref<string | null>(null)
  const loadStatus = ref(0)
  const serverErrors = ref<Record<string, string[]>>({})
  const storedDraft = ref<EditStoredDraft | null>(null)

  const id = computed(() => toValue(entityId))
  const storageKey = computed(
    () => `nextmoe-editor:draft:${config.family}:${id.value}`
  )

  const fields = computed<CatalogSchemaField[]>(() =>
    (schema.value?.fields ?? []).filter((field) => !field.deprecated)
  )
  const fieldByKey = computed(
    () => new Map(fields.value.map((field) => [field.key, field]))
  )
  const capabilityByKey = computed(
    () =>
      new Map<string, CatalogFieldCapability>(
        (capabilities.value?.fields ?? []).map((cap) => [cap.key, cap])
      )
  )

  const changedKeys = computed(() =>
    changedFieldKeys(fields.value, base.value, draft.value)
  )
  const isDirty = computed(() => changedKeys.value.length > 0)
  const patch = computed(() =>
    buildPatch(fields.value, base.value, draft.value)
  )
  const issues = computed<EditIssue[]>(() =>
    validateFields(
      fields.value,
      draft.value,
      changedKeys.value,
      vocabularies.byName.value
    )
  )
  const issuesByKey = computed(() => {
    const out: Record<string, EditIssue[]> = {}
    for (const issue of issues.value) {
      out[issue.key] = [...(out[issue.key] ?? []), issue]
    }
    return out
  })
  const blockedKeys = computed(() =>
    changedKeys.value.filter(
      (key) => !capabilityByKey.value.get(key)?.can_propose
    )
  )
  const willAutomerge = computed(
    () =>
      changedKeys.value.length > 0 &&
      changedKeys.value.every(
        (key) => capabilityByKey.value.get(key)?.would_automerge
      )
  )

  const fetchSnapshot = () =>
    api<CatalogSnapshot>(
      `/v2/moderation/snapshots/${config.family}/${id.value}`
    )

  const refreshQuota = async (force = false) => {
    if (
      force ||
      !quotaRequest ||
      Date.now() - quotaRequest.at > QUOTA_MAX_AGE_MS
    ) {
      quotaRequest = {
        at: Date.now(),
        promise: api<EditQuota>('/v2/me/edit-quota').catch(() => null)
      }
    }
    quota.value = await quotaRequest.promise
  }

  const fetchSchema = () => {
    let request = schemaRequests.get(config.family)
    if (!request) {
      request = api<CatalogObjectSchema>(
        `/v2/catalog/schemas/${config.family}`
      ).catch((error) => {
        schemaRequests.delete(config.family)
        throw error
      })
      schemaRequests.set(config.family, request)
    }
    return request
  }

  const load = async () => {
    loading.value = true
    loadError.value = null
    loadStatus.value = 0
    try {
      const [schemaRes, capRes, snapshot] = await Promise.all([
        fetchSchema(),
        api<CatalogEditCapabilities>(
          `/v2/me/edit-capabilities/${config.family}`,
          { query: { entity_id: id.value } }
        ).catch(() => null),
        fetchSnapshot(),
        refreshQuota()
      ])
      schema.value = schemaRes
      capabilities.value = capRes
      await vocabularies.load(vocabularyNamesOf(schemaRes.fields ?? []))
      base.value = snapshot.field_values ?? {}
      draft.value = cloneValue(base.value)
      serverErrors.value = {}
      const stored = persist ? readStorage(storageKey.value) : null
      storedDraft.value =
        stored && Object.keys(stored.values ?? {}).length > 0 ? stored : null
    } catch (error) {
      const problem = parseEditProblem(error)
      loadStatus.value = problem.status
      loadError.value = editProblemMessage(problem)
    } finally {
      loading.value = false
    }
  }

  const setValue = (key: string, value: unknown) => {
    draft.value = { ...draft.value, [key]: value }
    if (serverErrors.value[key]) {
      const { [key]: _, ...rest } = serverErrors.value
      serverErrors.value = rest
    }
  }

  const revertField = (key: string) => {
    setValue(key, cloneValue(base.value[key]))
    const companion = suppressedKeyOf(key)
    if (fieldByKey.value.has(companion)) {
      setValue(companion, cloneValue(base.value[companion]))
    }
  }

  const resetAll = () => {
    draft.value = cloneValue(base.value)
    serverErrors.value = {}
  }

  const titlesKey = `${config.entityType}.titles`
  watch(
    () => draft.value[titlesKey],
    (titles) => {
      const companion = suppressedKeyOf(titlesKey)
      if (!fieldByKey.value.has(companion)) {
        return
      }
      const next = reconcileTitleSuppression(
        titlesKey,
        base.value[titlesKey],
        titles,
        draft.value[companion]
      )
      if (!sameValue(next, draft.value[companion])) {
        draft.value = { ...draft.value, [companion]: next }
      }
    },
    { deep: true }
  )

  const baseFingerprintOf = (keys: string[]) =>
    fingerprint(keys.map((key) => [key, base.value[key] ?? null]))

  let saveTimer: ReturnType<typeof setTimeout> | null = null
  watch(
    draft,
    () => {
      if (!persist || loading.value || storedDraft.value) {
        return
      }
      if (saveTimer) {
        clearTimeout(saveTimer)
      }
      saveTimer = setTimeout(() => {
        const keys = changedKeys.value
        writeStorage(
          storageKey.value,
          keys.length
            ? {
                savedAt: new Date().toISOString(),
                baseFingerprint: baseFingerprintOf(keys),
                values: Object.fromEntries(
                  keys.map((key) => [key, draft.value[key]])
                )
              }
            : null
        )
      }, DRAFT_SAVE_DELAY_MS)
    },
    { deep: true }
  )

  const storedDraftIsStale = computed(() => {
    const stored = storedDraft.value
    return stored
      ? stored.baseFingerprint !== baseFingerprintOf(Object.keys(stored.values))
      : false
  })

  const restoreDraft = () => {
    const stored = storedDraft.value
    if (!stored) {
      return
    }
    const next = { ...draft.value }
    for (const [key, value] of Object.entries(stored.values)) {
      if (fieldByKey.value.has(key)) {
        next[key] = cloneValue(value)
      }
    }
    storedDraft.value = null
    draft.value = next
  }

  const discardDraft = () => {
    storedDraft.value = null
    writeStorage(storageKey.value, null)
  }

  const overlay = (values: EditFieldValues) => {
    const next = { ...draft.value }
    for (const [key, value] of Object.entries(values)) {
      if (fieldByKey.value.has(key)) {
        next[key] = cloneValue(value)
      }
    }
    draft.value = next
  }

  const findConflicts = async () => {
    const fresh = await fetchSnapshot().catch(() => null)
    if (!fresh) {
      return []
    }
    return changedKeys.value.filter(
      (key) => !sameValue(fresh.field_values?.[key], base.value[key])
    )
  }

  let idempotencyKey: string | null = null
  let idempotencyPatch = ''

  const submit = async (note: string) => {
    const body = {
      entity_type: config.entityType,
      entity_id: id.value,
      patch: patch.value,
      note
    }
    const signature = stableStringify(body)
    if (!idempotencyKey || signature !== idempotencyPatch) {
      idempotencyKey = crypto.randomUUID()
      idempotencyPatch = signature
    }
    try {
      const proposal = await api<CatalogProposal>('/v2/me/proposals', {
        method: 'POST',
        body,
        headers: { 'Idempotency-Key': idempotencyKey }
      })
      idempotencyKey = null
      quotaRequest = null
      writeStorage(storageKey.value, null)
      serverErrors.value = {}
      return { ok: true as const, proposal }
    } catch (error) {
      const problem = parseEditProblem(error)
      serverErrors.value = editProblemFieldErrors(problem)
      if (problem.status && problem.status < 500) {
        idempotencyKey = null
      }
      return { ok: false as const, message: editProblemMessage(problem) }
    }
  }

  const acceptSubmitted = () => {
    base.value = cloneValue(draft.value)
  }

  return {
    schema,
    capabilities,
    quota,
    base,
    draft,
    loading,
    loadError,
    loadStatus,
    serverErrors,
    storedDraft,
    storedDraftIsStale,
    fields,
    fieldByKey,
    capabilityByKey,
    changedKeys,
    isDirty,
    patch,
    issues,
    issuesByKey,
    blockedKeys,
    willAutomerge,
    vocabularies: vocabularies.byName,
    load,
    refreshQuota,
    setValue,
    revertField,
    resetAll,
    restoreDraft,
    discardDraft,
    overlay,
    findConflicts,
    submit,
    acceptSubmitted
  }
}

export type EntityEditor = ReturnType<typeof useEntityEditor>
