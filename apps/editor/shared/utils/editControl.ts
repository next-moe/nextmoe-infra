import type { CatalogSchemaField } from '../types/catalog'
import type { EditControl, EditFieldConfig } from '../types/edit'

export const resolveEditControl = (
  field: CatalogSchemaField,
  config?: EditFieldConfig
): EditControl => {
  if (config?.control) {
    return config.control
  }
  switch (field.field_type) {
    case 'text':
      return 'text'
    case 'enum':
      return field.vocabulary ? 'vocab' : 'readonly'
    case 'int':
      return 'int'
    case 'ref':
      return 'ref'
    case 'date':
      return 'date'
    case 'imagehash':
      return 'image'
    case 'list':
      if (field.element?.type === 'object') {
        return 'objectList'
      }
      if (field.element?.type === 'ref') {
        return 'refList'
      }
      if (field.element?.type === 'text') {
        return field.vocabulary || config?.vocabulary
          ? 'vocabList'
          : 'stringList'
      }
      return 'readonly'
    default:
      return 'readonly'
  }
}
